# Whisper.cpp セットアップ手順

## 前提条件
- Gitがインストールされていない場合は、まずGitをセットアップしてください。

## 1. Go言語のインストール
[Go公式サイト](https://go.dev/doc/install)からダウンロードしてインストール

## 2. FFmpegのインストール
音声ファイルの編集・解析のために必要です。
- [FFmpeg for macOS](https://evermeet.cx/ffmpeg/)からダウンロード

## 3. CMakeのインストール
実行ファイル作成のために必要です。
- [CMake公式サイト](https://cmake.org/download/)からダウンロードしてインストール

## 4. Visual Studio Build Toolsのインストール
C++が使えるように「C++によるデスクトップ開発」を選択してダウンロード
- [Visual Studio Build Tools](https://visualstudio.microsoft.com/ja/visual-cpp-build-tools/)

## 5. Whisper.cppのクローン
```bash
git clone https://github.com/ggerganov/whisper.cpp
```

## 6. モデルファイルのダウンロード
```bash
cd whisper.cpp/models
```

以下のリンクにアクセスしてモデルファイルをダウンロード（自動でダウンロードが開始されます）
- [ggml-large-v3.bin](https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3.bin)

ダウンロード完了後、ファイルを `whisper.cpp/models` 内に配置してください。

## 7. ビルド用フォルダの作成とビルド
whisper.cppディレクトリ内にビルド用フォルダを作成し、以下のコマンドを実行：

```bash
mkdir build
cd build
cmake .. -G "Visual Studio 17 2022" -A x64
cmake --build .
```

## 8. 実行
セットアップが完了したら、以下のコマンドで実行：

```bash
go run main.go [サンプルファイル] [output]
```

---

これでWhisper.cppを使った音声認識システムのセットアップが完了です。

# いじってみてほしいところ

```  main.go 48
numCPU := runtime.NumCPU()
maxWorkers := numCPU
```

CPUの値によってもっともよいパフォーマンスがあると思うので、実際に使用するとよい。
``` main.go 61,62
    MinSegmentDuration: 20.0,
    MaxSegmentDuration: 30.0,
```
ここで一つ一つの音声ファイルの長さを変更できます。

``` main.go 308
initialPrompt := `この音声は大学のサークル活動に関する会話です。
					内容には「理科大（りかだい）」または「理大（りだい）」という大学名が登場します。
					また、「理大祭（りだいさい）」というイベント名が含まれる場合があります。
					会話は自然な日本語で行われており、学生同士のカジュアルなやり取りが含まれます。
					固有名詞（大学名・イベント名など）は正確に認識してください。
					日本語の音声の認識を行います`
```

内容に応じて使うときに変えてみてください。


