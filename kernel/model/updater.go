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
	"fmt"
	"sync"

	"github.com/siyuan-note/siyuan/kernel/util"
)

var (
	checkUpdateLock = &sync.Mutex{}
)

func CheckUpdate(showMsg bool) {
	if !showMsg {
		return
	}

	if "ios" == util.Container {
		if showMsg {
			util.PushMsg(Conf.Language(36), 5000)
		}
		return
	}

	checkUpdateLock.Lock()
	defer checkUpdateLock.Unlock()

	result, err := util.GetRhyResult(showMsg, Conf.System.NetworkProxy.String())
	if nil != err {
		return
	}

	ver := result["ver"].(string)
	release := result["release"].(string)
	var msg string
	var timeout int
	if ver == util.Ver {
		msg = Conf.Language(10)
		timeout = 3000
	} else {
		msg = fmt.Sprintf(Conf.Language(9), "<a href=\""+release+"\">"+release+"</a>")
		showMsg = true
		timeout = 15000
	}
	if showMsg {
		util.PushMsg(msg, timeout)
	}
}