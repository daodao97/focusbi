package engine

import "testing"

func TestArrayTableUnionSameCols(t *testing.T) {
	tb := newArrayTable([]string{"a", "b"}, []map[string]any{{"a": 1, "b": 2}})
	tb.union([]string{"a", "b"}, []map[string]any{{"a": 3, "b": 4}})
	if len(tb.rows) != 2 || tb.rows[1]["a"] != 3 {
		t.Fatalf("union 行数/值错误: %+v", tb.rows)
	}
}

func TestArrayTableUnionColUnion(t *testing.T) {
	// 列不一致: 取并集, 缺列补空。
	tb := newArrayTable([]string{"a"}, []map[string]any{{"a": 1}})
	tb.union([]string{"a", "b"}, []map[string]any{{"a": 2, "b": 9}})
	if len(tb.cols) != 2 {
		t.Fatalf("列并集应为 [a b], got %+v", tb.cols)
	}
	if tb.rows[0]["b"] != "" {
		t.Errorf("基底行缺列应补空, got %v", tb.rows[0]["b"])
	}
	if tb.rows[1]["b"] != 9 {
		t.Errorf("并入行 b 应为 9, got %v", tb.rows[1]["b"])
	}
}

func TestArrayTableJoinLeft(t *testing.T) {
	tb := newArrayTable([]string{"id", "name"}, []map[string]any{
		{"id": 1, "name": "a"},
		{"id": 2, "name": "b"},
	})
	tb.join([]string{"id", "amount"}, []map[string]any{
		{"id": 1, "amount": 100},
	}, []string{"id"}, false)
	if !tb.hasCol("amount") {
		t.Fatal("join 后应有 amount 列")
	}
	if tb.rows[0]["amount"] != 100 {
		t.Errorf("id=1 应连上 amount=100, got %v", tb.rows[0]["amount"])
	}
	if tb.rows[1]["amount"] != "" {
		t.Errorf("id=2 无匹配应补空, got %v", tb.rows[1]["amount"])
	}
	if len(tb.rows) != 2 {
		t.Errorf("左连接行数不变, got %d", len(tb.rows))
	}
}

func TestArrayTableJoinFull(t *testing.T) {
	tb := newArrayTable([]string{"id", "name"}, []map[string]any{
		{"id": 1, "name": "a"},
	})
	tb.join([]string{"id", "amount"}, []map[string]any{
		{"id": 1, "amount": 100},
		{"id": 2, "amount": 200}, // 右表独有
	}, []string{"id"}, true)
	if len(tb.rows) != 2 {
		t.Fatalf("全连接应并入右表未匹配行, got %d 行: %+v", len(tb.rows), tb.rows)
	}
	// 找到 id=2 那行
	var got map[string]any
	for _, r := range tb.rows {
		if r["id"] == 2 {
			got = r
		}
	}
	if got == nil || got["amount"] != 200 || got["name"] != "" {
		t.Errorf("全连接右表独有行错误: %+v", got)
	}
}

func TestArrayTableJoinKeyIntersection(t *testing.T) {
	// 不指定 onKeys: 取列交集 (id) 作为连接键。
	tb := newArrayTable([]string{"id", "x"}, []map[string]any{{"id": 1, "x": "p"}})
	tb.join([]string{"id", "y"}, []map[string]any{{"id": 1, "y": "q"}}, nil, false)
	if tb.rows[0]["y"] != "q" {
		t.Errorf("列交集连接失败: %+v", tb.rows[0])
	}
}

func TestParseJoinConfig(t *testing.T) {
	// @union
	if spec, ok := parseJoinConfig(map[string]any{"union": true}); !ok || !spec.isUnion {
		t.Fatalf("union 解析失败: %+v ok=%v", spec, ok)
	}
	// @join=day,channel
	spec, ok := parseJoinConfig(map[string]any{"join": "day,channel"})
	if !ok || spec.isUnion || len(spec.onKeys) != 2 || spec.onKeys[0] != "day" {
		t.Fatalf("join 字符串解析失败: %+v", spec)
	}
	// @join={"on":"id","full":true}
	spec, ok = parseJoinConfig(map[string]any{"join": map[string]any{"on": "id", "full": true}})
	if !ok || len(spec.onKeys) != 1 || !spec.full {
		t.Fatalf("join 对象解析失败: %+v", spec)
	}
	// @join (无值 bool)
	if spec, ok := parseJoinConfig(map[string]any{"join": true}); !ok || len(spec.onKeys) != 0 {
		t.Fatalf("无值 join 应 ok 且无 keys: %+v", spec)
	}
	// 无 join/union -> 不参与
	if _, ok := parseJoinConfig(map[string]any{"id": "x"}); ok {
		t.Fatal("无 join/union 应 ok=false")
	}
}
