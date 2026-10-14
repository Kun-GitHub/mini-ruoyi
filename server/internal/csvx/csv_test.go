// 有问题欢迎联系 hot_kun@hotmail.com
package csvx

import (
	"bytes"
	"strings"
	"testing"
)

func TestEscapeNeutralizesFormulaInjection(t *testing.T) {
	// 前四行是 Excel 认的公式起始符，第 5、6 行是同样能拼出公式的控制字符。
	cases := []struct{ in, want string }{
		{"=1+1", "'=1+1"},
		{"+SUM(A1)", "'+SUM(A1)"},
		{"-2+3", "'-2+3"},
		{"@SUM(A1)", "'@SUM(A1)"},
		{"\t=1+1", "'\t=1+1"},
		{"\r=1+1", "'\r=1+1"},
		// 正常内容一个字节都不该动
		{"alice", "alice"},
		{"张伟", "张伟"},
		{"2026-10-08T06:37:24Z", "2026-10-08T06:37:24Z"},
		{"", ""},
	}
	for _, c := range cases {
		if got := Escape(c.in); got != c.want {
			t.Errorf("Escape(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestEscapeUnescapeRoundTrip(t *testing.T) {
	// Unescape(Escape(x)) 必须等于 x，否则「导出 → 改 → 导回」会悄悄改值。
	// 重点是最后一组：值本身就带引号的那几个。Escape 对它们会再多加一个引号，
	// 否则 `'+86` 与「`+86` 被转义后的样子」长得一模一样，Unescape 无法区分该不该剥。
	cases := []string{
		"+8613800000000",
		"=1+1",
		"-2+3",
		"@SUM(A1)",
		"\t=1+1",
		"\r=1+1",
		"alice",
		"张伟",
		"13800000000",
		"2026-10-08T06:37:24Z",
		"",
		"'",
		"'abc", // 引号后面不是公式前缀：引号是数据
		"'+86",
		"''=1",
		"'''@x",
	}
	for _, in := range cases {
		if got := Unescape(Escape(in)); got != in {
			t.Errorf("Unescape(Escape(%q)) = %q，没有还原", in, got)
		}
	}
}

func TestWriteAddsBOMAndEscapes(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.Write([]string{"username", "nickname"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Write([]string{"=cmd", "张伟"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	w.Flush()
	if err := w.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}

	got := buf.String()
	if !strings.HasPrefix(got, BOM) {
		t.Errorf("输出没有以 BOM 开头：%q", got)
	}
	if n := strings.Count(got, BOM); n != 1 {
		t.Errorf("BOM 出现 %d 次，期望 1 次（只在最开头）", n)
	}
	if !strings.Contains(got, "'=cmd") {
		t.Errorf("公式没有被中和：%q", got)
	}
	// BOM 必须在第一行之前，不能在中间
	if !strings.HasPrefix(strings.TrimPrefix(got, BOM), "username,nickname") {
		t.Errorf("第一行不是表头：%q", got)
	}
}

func TestWriteQuotesFieldsContainingSeparators(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.Write([]string{"a,b", "带\"引号\"", "换\n行"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	w.Flush()

	body := strings.TrimPrefix(buf.String(), BOM)
	if body != "\"a,b\",\"带\"\"引号\"\"\",\"换\n行\"\n" {
		t.Errorf("RFC 4180 转义结果不对：%q", body)
	}
}

func TestReadStripsHeaderBOMAndParsesQuotes(t *testing.T) {
	body := BOM + "username,nickname\n\"a,b\",张伟\n"
	header, rows, err := Read(strings.NewReader(body), 100)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(header) != 2 || header[0] != "username" {
		t.Errorf("表头 = %q，期望第一列是去掉 BOM 的 username", header)
	}
	if len(rows) != 1 || rows[0][0] != "a,b" || rows[0][1] != "张伟" {
		t.Errorf("数据行 = %q", rows)
	}
}

func TestReadRejectsTooManyRows(t *testing.T) {
	_, rows, err := Read(strings.NewReader("a\n1\n2\n3\n"), 2)
	if err != ErrTooManyRows {
		t.Errorf("err = %v，期望 ErrTooManyRows（不能默默截断）", err)
	}
	if rows != nil {
		t.Errorf("超限时应返回 nil 行，实际返回 %d 行", len(rows))
	}
	// 正好等于上限要放过
	if _, rows, err := Read(strings.NewReader("a\n1\n2\n"), 2); err != nil || len(rows) != 2 {
		t.Errorf("正好上限时 err = %v, rows = %d，期望 nil / 2", err, len(rows))
	}
}

func TestReadRejectsEmptyAndRaggedFiles(t *testing.T) {
	if _, _, err := Read(strings.NewReader(""), 10); err != ErrEmpty {
		t.Errorf("空文件 err = %v，期望 ErrEmpty", err)
	}
	// 列数与表头不一致：必须报错，否则数据会错位写进库
	if _, _, err := Read(strings.NewReader("a,b\n1\n"), 10); err == nil {
		t.Error("列数不一致的文件没有报错")
	}
}
