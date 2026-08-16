package unzip

import (
	"archive/zip"
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipEntry はテスト用にメモリ上で組み立てるZIPエントリを表す。
type zipEntry struct {
	name string
	data []byte
}

// 通常のZIPが、同名の展開先フォルダへファイル・サブフォルダごと展開されることを確認する。
func TestProcessFilesExtractsNormalZIP(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "images.zip")
	writeZIP(t, archive, []zipEntry{{"001.jpg", []byte("image")}, {"nested/readme.txt", []byte("hello")}})

	batch := ProcessFiles([]string{archive}, testOptions())
	if batch.Success != 1 || batch.Failed != 0 {
		t.Fatalf("success=%d failed=%d, want 1/0", batch.Success, batch.Failed)
	}
	assertFileContent(t, filepath.Join(dir, "images", "001.jpg"), "image")
	assertFileContent(t, filepath.Join(dir, "images", "nested", "readme.txt"), "hello")
	// 既定では元ZIPを削除しない安全仕様も同時に確認する。
	assertExists(t, archive)
}

// ZIP内にあるZIPを、親の展開先に作られた同名フォルダへ再帰展開できることを確認する。
func TestProcessFilesExtractsNestedZIP(t *testing.T) {
	dir := t.TempDir()
	inner := zipBytes(t, []zipEntry{{"inside.txt", []byte("inside")}})
	archive := filepath.Join(dir, "outer.zip")
	writeZIP(t, archive, []zipEntry{{"sub.zip", inner}})

	batch := ProcessFiles([]string{archive}, testOptions())
	if batch.Success != 1 || batch.Results[0].ExtractedZIPs != 2 {
		t.Fatalf("batch=%+v, want root and nested ZIP extracted", batch)
	}
	assertFileContent(t, filepath.Join(dir, "outer", "sub", "inside.txt"), "inside")
	// 内部ZIPも既定では残るため、展開済みフォルダとZIPが共存する。
	assertExists(t, filepath.Join(dir, "outer", "sub.zip"))
}

// 探索中に新たに現れたZIPも次の周回で見つけ、3階層まで処理できることを確認する。
func TestProcessFilesExtractsThreeNestedLevels(t *testing.T) {
	dir := t.TempDir()
	third := zipBytes(t, []zipEntry{{"001.jpg", []byte("pixel")}})
	second := zipBytes(t, []zipEntry{{"inner.zip", third}})
	first := filepath.Join(dir, "images.zip")
	writeZIP(t, first, []zipEntry{{"sub.zip", second}})

	batch := ProcessFiles([]string{first}, testOptions())
	if batch.Success != 1 || batch.Results[0].ExtractedZIPs != 3 {
		t.Fatalf("batch=%+v, want 3 extracted ZIPs", batch)
	}
	assertFileContent(t, filepath.Join(dir, "images", "sub", "inner", "001.jpg"), "pixel")
}

// 複数ZIPは処理しつつ、Explorerから混在して渡るZIP以外の項目を無視できることを確認する。
func TestProcessFilesHandlesMultipleZIPsAndIgnoresNonZIP(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.zip")
	b := filepath.Join(dir, "b.ZIP")
	note := filepath.Join(dir, "note.txt")
	writeZIP(t, a, []zipEntry{{"a.txt", []byte("a")}})
	writeZIP(t, b, []zipEntry{{"b.txt", []byte("b")}})
	if err := os.WriteFile(note, []byte("not zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	batch := ProcessFiles([]string{a, note, b}, testOptions())
	if batch.Success != 2 || batch.Failed != 0 || len(batch.Results) != 2 {
		t.Fatalf("batch=%+v, want two successful ZIPs", batch)
	}
	assertFileContent(t, filepath.Join(dir, "a", "a.txt"), "a")
	assertFileContent(t, filepath.Join(dir, "b", "b.txt"), "b")
}

// 壊れたZIPがあっても、後続の最上位ZIPの処理を継続することを確認する。
func TestProcessFilesContinuesAfterBrokenZIP(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.zip")
	valid := filepath.Join(dir, "valid.zip")
	if err := os.WriteFile(broken, []byte("this is not a ZIP"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeZIP(t, valid, []zipEntry{{"ok.txt", []byte("ok")}})

	batch := ProcessFiles([]string{broken, valid}, testOptions())
	if batch.Success != 1 || batch.Failed != 1 {
		t.Fatalf("batch=%+v, want one success and one failure", batch)
	}
	assertFileContent(t, filepath.Join(dir, "valid", "ok.txt"), "ok")
}

// 既存の展開先では、既定の上書きモードが同名ファイルをZIPの内容で更新することを確認する。
func TestExistingDestinationCanBeOverwritten(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "same.zip")
	writeZIP(t, archive, []zipEntry{{"value.txt", []byte("new")}})
	destination := filepath.Join(dir, "same")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "value.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	batch := ProcessFiles([]string{archive}, testOptions())
	if batch.Success != 1 {
		t.Fatalf("batch=%+v", batch)
	}
	assertFileContent(t, filepath.Join(destination, "value.txt"), "new")
}

// "../../" を含むZIPエントリが展開先の親へ書き出されないことを確認する。
func TestProcessFilesPreventsZipSlip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "unsafe.zip")
	writeZIP(t, archive, []zipEntry{{"../../escape.txt", []byte("escape")}, {"ok.txt", []byte("ok")}})

	batch := ProcessFiles([]string{archive}, testOptions())
	if batch.Success != 0 || batch.Failed != 1 {
		t.Fatalf("batch=%+v, want unsafe ZIP to fail", batch)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("Zip Slip created a file outside the extraction directory: %v", err)
	}
}

// 日本語および空白を含むZIP名・フォルダ名・ファイル名を扱えることを確認する。
func TestProcessFilesSupportsJapaneseNames(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "画像 データ.zip")
	writeZIP(t, archive, []zipEntry{{"写真/こんにちは.txt", []byte("日本語")}})

	batch := ProcessFiles([]string{archive}, testOptions())
	if batch.Success != 1 {
		t.Fatalf("batch=%+v", batch)
	}
	assertFileContent(t, filepath.Join(dir, "画像 データ", "写真", "こんにちは.txt"), "日本語")
}

// エントリを持たない空ZIPでも、空の展開先フォルダを作って成功することを確認する。
func TestProcessFilesHandlesEmptyZIP(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "empty.zip")
	writeZIP(t, archive, nil)

	batch := ProcessFiles([]string{archive}, testOptions())
	if batch.Success != 1 || batch.Results[0].ExtractedFiles != 0 {
		t.Fatalf("batch=%+v, want empty ZIP to succeed", batch)
	}
	assertDir(t, filepath.Join(dir, "empty"))
}

// --delete-zip 相当の指定時だけ、展開成功後に元ZIPが削除されることを確認する。
func TestProcessFilesDeletesZIPWhenRequested(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "remove-me.zip")
	writeZIP(t, archive, []zipEntry{{"file.txt", []byte("ok")}})

	options := testOptions()
	options.DeleteZIP = true
	batch := ProcessFiles([]string{archive}, options)
	if batch.Success != 1 {
		t.Fatalf("batch=%+v", batch)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("archive still exists or stat failed: %v", err)
	}
}

// Windows固有のドライブ指定とバックスラッシュ経路も、Zip Slipとして拒否することを確認する。
func TestSecureOutputPathRejectsWindowsDriveAndBackslashTraversal(t *testing.T) {
	root := t.TempDir()
	for _, entryName := range []string{"C:\\Windows\\system32\\bad.txt", "..\\outside.txt", "/absolute.txt"} {
		if _, err := secureOutputPath(root, entryName); err == nil {
			t.Fatalf("secureOutputPath(%q) succeeded, want rejection", entryName)
		}
	}
}

// ZIP内のシンボリックリンクを通常ファイルとして展開せず、処理全体を失敗させることを確認する。
func TestProcessFilesRejectsZIPSymlinkEntry(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "symlink-entry.zip")
	buffer := new(bytes.Buffer)
	writer := zip.NewWriter(buffer)
	// リンク先に"../outside"を設定し、リンクを許すと展開先外へ到達し得るZIPを作る。
	header := &zip.FileHeader{Name: "redirect"}
	header.SetMode(os.ModeSymlink | 0o777)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("../outside")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, buffer.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	batch := ProcessFiles([]string{archive}, testOptions())
	if batch.Success != 0 || batch.Failed != 1 {
		t.Fatalf("batch=%+v, want symlink ZIP entry to be rejected", batch)
	}
}

// 上書き先に既存のシンボリックリンクがある場合も、リンク先への書き込みを拒否することを確認する。
func TestProcessFilesRejectsPreexistingSymlinkedDirectory(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "linked.zip")
	writeZIP(t, archive, []zipEntry{{"redirect/escape.txt", []byte("blocked")}})
	destination := filepath.Join(dir, "linked")
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	// Windowsの権限設定などでリンクを作れない環境では、この防御テストだけをスキップする。
	if err := os.Symlink(outside, filepath.Join(destination, "redirect")); err != nil {
		t.Skipf("this environment does not allow creating a symbolic link: %v", err)
	}

	batch := ProcessFiles([]string{archive}, testOptions())
	if batch.Success != 0 || batch.Failed != 1 {
		t.Fatalf("batch=%+v, want symlinked directory to be rejected", batch)
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("write escaped through symbolic link: %v", err)
	}
}

// testOptions は、ログを捨てて上書きモードを使う共通のテスト設定を返す。
func testOptions() Options {
	return Options{Policy: Overwrite, Logger: log.New(io.Discard, "", 0)}
}

// writeZIP は、エントリ一覧から作ったテスト用ZIPを指定パスへ書き込む。
func writeZIP(t *testing.T, path string, entries []zipEntry) {
	t.Helper()
	if err := os.WriteFile(path, zipBytes(t, entries), 0o644); err != nil {
		t.Fatal(err)
	}
}

// zipBytes は、入れ子ZIPも作れるようにアーカイブ全体をバイト列として返す。
func zipBytes(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	buffer := new(bytes.Buffer)
	writer := zip.NewWriter(buffer)
	for _, entry := range entries {
		file, err := writer.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// assertFileContent は、指定ファイルが存在し、内容が期待値と完全一致することを確認する。
func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if got := string(data); got != want {
		t.Fatalf("content of %s = %q, want %q", path, got, want)
	}
}

// assertExists は、削除しない仕様の元ZIPなどが残っていることを確認する。
func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

// assertDir は、展開先が通常ファイルではなくディレクトリとして作られたことを確認する。
func assertDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected directory %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", path)
	}
}

// "folder/../file" のように正規化後も展開先内に収まる経路は、安全な経路として許可されることを確認する。
func TestNoUnexpectedParentTraversalInExtractedTree(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "safe.zip")
	writeZIP(t, archive, []zipEntry{{"folder/../still-safe.txt", []byte("safe")}})

	batch := ProcessFiles([]string{archive}, testOptions())
	if batch.Success != 1 {
		t.Fatalf("batch=%+v", batch)
	}
	assertFileContent(t, filepath.Join(dir, "safe", "still-safe.txt"), "safe")
}

// スキップモードでは既存フォルダの内容を保持し、新しいZIP内容を展開しないことを確認する。
func TestSkipExistingDestination(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "already.zip")
	writeZIP(t, archive, []zipEntry{{"new.txt", []byte("new")}})
	destination := filepath.Join(dir, "already")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	options := testOptions()
	options.Policy = Skip
	batch := ProcessFiles([]string{archive}, options)
	if batch.Success != 1 || !batch.Results[0].Skipped {
		t.Fatalf("batch=%+v, want skipped success", batch)
	}
	assertFileContent(t, filepath.Join(destination, "keep.txt"), "keep")
	if _, err := os.Stat(filepath.Join(destination, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new file should not have been extracted: %v", err)
	}
}

// 別名モードでは既存フォルダを壊さず、最初の連番フォルダへ展開することを確認する。
func TestRenameExistingDestination(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "copy.zip")
	writeZIP(t, archive, []zipEntry{{"new.txt", []byte("new")}})
	if err := os.Mkdir(filepath.Join(dir, "copy"), 0o755); err != nil {
		t.Fatal(err)
	}

	options := testOptions()
	options.Policy = Rename
	batch := ProcessFiles([]string{archive}, options)
	if batch.Success != 1 || !strings.HasSuffix(batch.Results[0].Destination, "copy (2)") {
		t.Fatalf("batch=%+v, want renamed destination", batch)
	}
	assertFileContent(t, filepath.Join(dir, "copy (2)", "new.txt"), "new")
}
