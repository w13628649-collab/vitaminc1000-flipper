package main

import (
	"bytes"
	"os"
	"testing"
)

// 仓库里的 tables_gen.go 要和源数据重新生成的一字不差。源数据(rea-test 调查工作区)
// 不在的机器上跳过
func TestGeneratedUpToDate(t *testing.T) {
	const root = "../../../rea-test/investigations/albion-online/static-data"
	if _, err := os.Stat(root); err != nil {
		t.Skip("没有静态数据调查目录:", err)
	}
	tb, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Render(tb)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../internal/gamedata/tables_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
		t.Fatal("internal/gamedata/tables_gen.go 和源数据对不上,重跑 go generate ./internal/gamedata")
	}
}

func TestLineOf(t *testing.T) {
	type lt struct {
		line string
		tier int
	}
	for in, want := range map[string]lt{
		"T8_METALBAR": {"ORE", 8}, "T4_CLOTH_LEVEL3": {"FIBER", 4}, "T2_PLANKS": {"WOOD", 2},
		"T8_ORE_LEVEL1": {}, "T5_LEATHER_LEVEL5": {}, "T1_CLOTH": {}, "T8_MEAL_SOUP": {},
	} {
		if l, tier := lineOf(in); l != want.line || tier != want.tier {
			t.Errorf("lineOf(%s) = %q %d, want %+v", in, l, tier, want)
		}
	}
}
