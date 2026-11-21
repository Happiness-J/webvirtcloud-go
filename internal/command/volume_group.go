package command

import (
	"fmt"
	"os/user"
	"strconv"
)

func VolumeGroupID(name string) (int, error) {
	group, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf("查找卷属组失败")
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil || gid < 0 {
		return 0, fmt.Errorf("卷属组无效")
	}
	return gid, nil
}