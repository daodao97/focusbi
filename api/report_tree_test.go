package api

import (
	"testing"

	"xproxy/dao"
)

func TestReportParentWouldCycle(t *testing.T) {
	parents := map[int]int{1: 0, 2: 1, 3: 2}
	if !reportParentWouldCycle(1, 3, parents) {
		t.Fatal("父目录移动到后代应判定成环")
	}
	if reportParentWouldCycle(3, 1, parents) {
		t.Fatal("子目录移动到祖先不应判定成环")
	}
	parents[1] = 3
	if !reportParentWouldCycle(9, 1, parents) {
		t.Fatal("已有坏环应 fail-closed")
	}
}

func TestFolderHasReadableChildTerminatesOnCycle(t *testing.T) {
	list := []*dao.ReportRecord{
		{Id: 1, ParentID: 2, Type: "folder"},
		{Id: 2, ParentID: 1, Type: "folder"},
	}
	if folderHasReadableChild(1, list, map[int]bool{}) {
		t.Fatal("环中没有可读节点时不应返回 true")
	}
}
