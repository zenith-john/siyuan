// SiYuan - Build Your Eternal Digital Garden
// Copyright (c) 2020-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package model

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/88250/gulu"
	"github.com/88250/protyle"
	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/filesys"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

var historyTicker = time.NewTicker(time.Minute * 10)

func AutoGenerateDocHistory() {
	ChangeHistoryTick(Conf.Editor.GenerateHistoryInterval)
	for {
		<-historyTicker.C
		generateDocHistory()
	}
}

func generateDocHistory() {
	if 1 > Conf.Editor.GenerateHistoryInterval {
		return
	}

	WaitForWritingFiles()
	syncLock.Lock()
	defer syncLock.Unlock()

	for _, box := range Conf.GetOpenedBoxes() {
		box.generateDocHistory0()
	}

	historyDir := filepath.Join(util.WorkspaceDir, "history")
	clearOutdatedHistoryDir(historyDir)

	// 以下部分是老版本的清理逻辑，暂时保留

	for _, box := range Conf.GetBoxes() {
		historyDir = filepath.Join(util.DataDir, box.ID, ".siyuan", "history")
		clearOutdatedHistoryDir(historyDir)
	}

	historyDir = filepath.Join(util.DataDir, "assets", ".siyuan", "history")
	clearOutdatedHistoryDir(historyDir)

	historyDir = filepath.Join(util.DataDir, ".siyuan", "history")
	clearOutdatedHistoryDir(historyDir)
}

func ChangeHistoryTick(minutes int) {
	if 0 >= minutes {
		minutes = 3600
	}
	historyTicker.Reset(time.Minute * time.Duration(minutes))
}

func ClearWorkspaceHistory() (err error) {
	historyDir := filepath.Join(util.WorkspaceDir, "history")
	if gulu.File.IsDir(historyDir) {
		if err = os.RemoveAll(historyDir); nil != err {
			util.LogErrorf("remove workspace history dir [%s] failed: %s", historyDir, err)
			return
		}
		util.LogInfof("removed workspace history dir [%s]", historyDir)
	}

	// 以下部分是老版本的清理逻辑，暂时保留

	notebooks, err := ListNotebooks()
	if nil != err {
		return
	}

	for _, notebook := range notebooks {
		boxID := notebook.ID
		historyDir := filepath.Join(util.DataDir, boxID, ".siyuan", "history")
		if !gulu.File.IsDir(historyDir) {
			continue
		}

		if err = os.RemoveAll(historyDir); nil != err {
			util.LogErrorf("remove notebook history dir [%s] failed: %s", historyDir, err)
			return
		}
		util.LogInfof("removed notebook history dir [%s]", historyDir)
	}

	historyDir = filepath.Join(util.DataDir, ".siyuan", "history")
	if gulu.File.IsDir(historyDir) {
		if err = os.RemoveAll(historyDir); nil != err {
			util.LogErrorf("remove data history dir [%s] failed: %s", historyDir, err)
			return
		}
		util.LogInfof("removed data history dir [%s]", historyDir)
	}
	historyDir = filepath.Join(util.DataDir, "assets", ".siyuan", "history")
	if gulu.File.IsDir(historyDir) {
		if err = os.RemoveAll(historyDir); nil != err {
			util.LogErrorf("remove assets history dir [%s] failed: %s", historyDir, err)
			return
		}
		util.LogInfof("removed assets history dir [%s]", historyDir)
	}
	return
}

func GetDocHistoryContent(historyPath string) (content string, err error) {
	if !gulu.File.IsExist(historyPath) {
		return
	}

	data, err := filesys.NoLockFileRead(historyPath)
	if nil != err {
		util.LogErrorf("read file [%s] failed: %s", historyPath, err)
		return
	}
	luteEngine := NewLute()
	historyTree, err := protyle.ParseJSONWithoutFix(luteEngine, data)
	if nil != err {
		util.LogErrorf("parse tree from file [%s] failed, remove it", historyPath)
		os.RemoveAll(historyPath)
		return
	}
	content = renderBlockMarkdown(historyTree.Root)
	return
}

func RollbackDocHistory(boxID, historyPath string) (err error) {
	if !gulu.File.IsExist(historyPath) {
		return
	}

	WaitForWritingFiles()
	syncLock.Lock()

	srcPath := historyPath
	var destPath string
	baseName := filepath.Base(historyPath)
	id := strings.TrimSuffix(baseName, ".sy")

	filesys.ReleaseFileLocks(filepath.Join(util.DataDir, boxID))
	workingDoc := treenode.GetBlockTree(id)
	if nil != workingDoc {
		if err = os.RemoveAll(filepath.Join(util.DataDir, boxID, workingDoc.Path)); nil != err {
			syncLock.Unlock()
			return
		}
	}

	destPath, err = getRollbackDockPath(boxID, historyPath)
	if nil != err {
		syncLock.Unlock()
		return
	}

	if err = gulu.File.Copy(srcPath, destPath); nil != err {
		syncLock.Unlock()
		return
	}
	syncLock.Unlock()

	RefreshFileTree()
	IncWorkspaceDataVer()
	return nil
}

func getRollbackDockPath(boxID, historyPath string) (destPath string, err error) {
	baseName := filepath.Base(historyPath)
	parentID := strings.TrimSuffix(filepath.Base(filepath.Dir(historyPath)), ".sy")
	parentWorkingDoc := treenode.GetBlockTree(parentID)
	if nil != parentWorkingDoc {
		// 父路径如果是文档，则恢复到父路径下
		parentDir := strings.TrimSuffix(parentWorkingDoc.Path, ".sy")
		parentDir = filepath.Join(util.DataDir, boxID, parentDir)
		if err = os.MkdirAll(parentDir, 0755); nil != err {
			return
		}
		destPath = filepath.Join(parentDir, baseName)
	} else {
		// 父路径如果不是文档，则恢复到笔记本根路径下
		destPath = filepath.Join(util.DataDir, boxID, baseName)
	}
	return
}

func RollbackAssetsHistory(historyPath string) (err error) {
	historyPath = filepath.Join(util.WorkspaceDir, historyPath)
	if !gulu.File.IsExist(historyPath) {
		return
	}

	from := historyPath
	to := filepath.Join(util.DataDir, "assets", filepath.Base(historyPath))

	if err = gulu.File.Copy(from, to); nil != err {
		util.LogErrorf("copy file [%s] to [%s] failed: %s", from, to, err)
		return
	}
	IncWorkspaceDataVer()
	return nil
}

func RollbackNotebookHistory(historyPath string) (err error) {
	if !gulu.File.IsExist(historyPath) {
		return
	}

	from := historyPath
	to := filepath.Join(util.DataDir, filepath.Base(historyPath))

	if err = gulu.File.Copy(from, to); nil != err {
		util.LogErrorf("copy file [%s] to [%s] failed: %s", from, to, err)
		return
	}

	RefreshFileTree()
	IncWorkspaceDataVer()
	return nil
}

type History struct {
	Time  string         `json:"time"`
	Items []*HistoryItem `json:"items"`
}

type HistoryItem struct {
	Title string `json:"title"`
	Path  string `json:"path"`
}

const maxHistory = 32

func GetDocHistory(boxID string) (ret []*History, err error) {
	ret = []*History{}

	historyDir := filepath.Join(util.WorkspaceDir, "history")
	if !gulu.File.IsDir(historyDir) {
		return
	}

	historyBoxDirs, err := filepath.Glob(historyDir + "/*/" + boxID)
	if nil != err {
		util.LogErrorf("read dir [%s] failed: %s", historyDir, err)
		return
	}
	sort.Slice(historyBoxDirs, func(i, j int) bool {
		return historyBoxDirs[i] > historyBoxDirs[j]
	})

	luteEngine := NewLute()
	count := 0
	for _, historyBoxDir := range historyBoxDirs {
		var docs []*HistoryItem
		itemCount := 0
		filepath.Walk(historyBoxDir, func(path string, info fs.FileInfo, err error) error {
			if info.IsDir() {
				return nil
			}

			if !strings.HasSuffix(info.Name(), ".sy") {
				return nil
			}

			data, err := filesys.NoLockFileRead(path)
			if nil != err {
				util.LogErrorf("read file [%s] failed: %s", path, err)
				return nil
			}
			historyTree, err := protyle.ParseJSONWithoutFix(luteEngine, data)
			if nil != err {
				util.LogErrorf("parse tree from file [%s] failed, remove it", path)
				os.RemoveAll(path)
				return nil
			}
			historyName := historyTree.Root.IALAttr("title")
			if "" == historyName {
				historyName = info.Name()
			}

			docs = append(docs, &HistoryItem{
				Title: historyTree.Root.IALAttr("title"),
				Path:  path,
			})
			itemCount++
			if maxHistory < itemCount {
				return io.EOF
			}
			return nil
		})

		if 1 > len(docs) {
			continue
		}

		timeDir := filepath.Base(filepath.Dir(historyBoxDir))
		t := timeDir[:strings.LastIndex(timeDir, "-")]
		if ti, parseErr := time.Parse("2006-01-02-150405", t); nil == parseErr {
			t = ti.Format("2006-01-02 15:04:05")
		}

		ret = append(ret, &History{
			Time:  t,
			Items: docs,
		})

		count++
		if maxHistory <= count {
			break
		}
	}

	sort.Slice(ret, func(i, j int) bool {
		return ret[i].Time > ret[j].Time
	})
	return
}

func GetNotebookHistory() (ret []*History, err error) {
	ret = []*History{}

	historyDir := filepath.Join(util.WorkspaceDir, "history")
	if !gulu.File.IsDir(historyDir) {
		return
	}

	historyNotebookConfs, err := filepath.Glob(historyDir + "/*-delete/*/.siyuan/conf.json")
	if nil != err {
		util.LogErrorf("read dir [%s] failed: %s", historyDir, err)
		return
	}
	sort.Slice(historyNotebookConfs, func(i, j int) bool {
		iTimeDir := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(historyNotebookConfs[i]))))
		jTimeDir := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(historyNotebookConfs[j]))))
		return iTimeDir > jTimeDir
	})

	historyCount := 0
	for _, historyNotebookConf := range historyNotebookConfs {
		timeDir := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(historyNotebookConf))))
		t := timeDir[:strings.LastIndex(timeDir, "-")]
		if ti, parseErr := time.Parse("2006-01-02-150405", t); nil == parseErr {
			t = ti.Format("2006-01-02 15:04:05")
		}

		var c conf.BoxConf
		data, readErr := os.ReadFile(historyNotebookConf)
		if nil != readErr {
			util.LogErrorf("read notebook conf [%s] failed: %s", historyNotebookConf, readErr)
			continue
		}
		if err = json.Unmarshal(data, &c); nil != err {
			util.LogErrorf("parse notebook conf [%s] failed: %s", historyNotebookConf, err)
			continue
		}

		ret = append(ret, &History{
			Time: t,
			Items: []*HistoryItem{
				{
					Title: c.Name,
					Path:  filepath.Dir(filepath.Dir(historyNotebookConf)),
				},
			},
		})

		historyCount++
		if maxHistory <= historyCount {
			break
		}
	}

	sort.Slice(ret, func(i, j int) bool {
		return ret[i].Time > ret[j].Time
	})
	return
}

func GetAssetsHistory() (ret []*History, err error) {
	ret = []*History{}

	historyDir := filepath.Join(util.WorkspaceDir, "history")
	if !gulu.File.IsDir(historyDir) {
		return
	}

	historyAssetsDirs, err := filepath.Glob(historyDir + "/*/assets")
	if nil != err {
		util.LogErrorf("read dir [%s] failed: %s", historyDir, err)
		return
	}
	sort.Slice(historyAssetsDirs, func(i, j int) bool {
		return historyAssetsDirs[i] > historyAssetsDirs[j]
	})

	historyCount := 0
	for _, historyAssetsDir := range historyAssetsDirs {
		var assets []*HistoryItem
		itemCount := 0
		filepath.Walk(historyAssetsDir, func(path string, info fs.FileInfo, err error) error {
			if isSkipFile(info.Name()) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if info.IsDir() {
				return nil
			}

			assets = append(assets, &HistoryItem{
				Title: info.Name(),
				Path:  filepath.ToSlash(strings.TrimPrefix(path, util.WorkspaceDir)),
			})
			itemCount++
			if maxHistory < itemCount {
				return io.EOF
			}
			return nil
		})

		if 1 > len(assets) {
			continue
		}

		timeDir := filepath.Base(filepath.Dir(historyAssetsDir))
		t := timeDir[:strings.LastIndex(timeDir, "-")]
		if ti, parseErr := time.Parse("2006-01-02-150405", t); nil == parseErr {
			t = ti.Format("2006-01-02 15:04:05")
		}

		ret = append(ret, &History{
			Time:  t,
			Items: assets,
		})

		historyCount++
		if maxHistory <= historyCount {
			break
		}
	}

	sort.Slice(ret, func(i, j int) bool {
		return ret[i].Time > ret[j].Time
	})
	return
}

func (box *Box) generateDocHistory0() {
	files := box.recentModifiedDocs()
	if 1 > len(files) {
		return
	}

	historyDir, err := util.GetHistoryDir("update")
	if nil != err {
		util.LogErrorf("get history dir failed: %s", err)
		return
	}

	for _, file := range files {
		historyPath := filepath.Join(historyDir, box.ID, strings.TrimPrefix(file, filepath.Join(util.DataDir, box.ID)))
		if err = os.MkdirAll(filepath.Dir(historyPath), 0755); nil != err {
			util.LogErrorf("generate history failed: %s", err)
			return
		}

		var data []byte
		if data, err = filesys.NoLockFileRead(file); err != nil {
			util.LogErrorf("generate history failed: %s", err)
			return
		}

		if err = gulu.File.WriteFileSafer(historyPath, data, 0644); err != nil {
			util.LogErrorf("generate history failed: %s", err)
			return
		}
	}
	return
}

func clearOutdatedHistoryDir(historyDir string) {
	if !gulu.File.IsExist(historyDir) {
		return
	}

	dirs, err := os.ReadDir(historyDir)
	if nil != err {
		util.LogErrorf("clear history [%s] failed: %s", historyDir, err)
		return
	}

	now := time.Now()
	var removes []string
	for _, dir := range dirs {
		dirInfo, err := dir.Info()
		if nil != err {
			util.LogErrorf("read history dir [%s] failed: %s", dir.Name(), err)
			continue
		}
		if Conf.Editor.HistoryRetentionDays < int(now.Sub(dirInfo.ModTime()).Hours()/24) {
			removes = append(removes, filepath.Join(historyDir, dir.Name()))
		}
	}
	for _, dir := range removes {
		if err = os.RemoveAll(dir); nil != err {
			util.LogErrorf("remove history dir [%s] failed: %s", err)
			continue
		}
		//util.LogInfof("auto removed history dir [%s]", dir)
	}
}

var boxLatestHistoryTime = map[string]time.Time{}

func (box *Box) recentModifiedDocs() (ret []string) {
	latestHistoryTime := boxLatestHistoryTime[box.ID]
	filepath.Walk(filepath.Join(util.DataDir, box.ID), func(path string, info fs.FileInfo, err error) error {
		if nil == info {
			return nil
		}
		if isSkipFile(info.Name()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			return nil
		}

		if info.ModTime().After(latestHistoryTime) {
			ret = append(ret, filepath.Join(path))
		}
		return nil
	})
	box.UpdateHistoryGenerated()
	return
}