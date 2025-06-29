package utils

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func Clean() {
	// 対象フォルダ
	dir := "./output"

	// 削除対象の拡張子（必要に応じて追加）
	extensions := []string{".mp3", ".wav", ".m4a", ".flac", ".aac"}

	// フォルダ内のファイルを走査
	err := filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// ディレクトリは無視
		if info.IsDir() {
			return nil
		}

		// 拡張子チェック
		for _, ext := range extensions {
			if strings.HasSuffix(strings.ToLower(info.Name()), ext) {
				fmt.Printf("削除: %s\n", path)
				if err := os.Remove(path); err != nil {
					log.Printf("削除失敗: %s (%v)", path, err)
				}
				break
			}
		}
		return nil
	})

	if err != nil {
		log.Fatalf("エラー: %v", err)
	}
}
