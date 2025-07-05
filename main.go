package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// AudioSegment represents a split audio segment
type AudioSegment struct {
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
	Filename  string  `json:"filename"`
	FilePath  string  `json:"file_path"`
	Duration  float64 `json:"duration"`
}

// TranscriptionResult represents the result from Python Whisper
type TranscriptionResult struct {
	Segment AudioSegment `json:"segment"`
	Text    string       `json:"text"`
	Error   string       `json:"error,omitempty"`
}

// AudioAnalyzer handles the entire audio processing pipeline
type AudioAnalyzer struct {
	InputFile          string
	OutputDir          string
	MaxWorkers         int
	SilenceThreshold   string // ffmpegの無音検出閾値
	SilenceDuration    string // 無音継続時間
	MinSegmentDuration float64
	MaxSegmentDuration float64 // 最大セグメント長（秒）
}

// NewAudioAnalyzer creates a new AudioAnalyzer instance
func NewAudioAnalyzer(inputFile, outputDir string) *AudioAnalyzer {
	// CPUコア数を取得し、最適なワーカー数を設定
	numCPU := runtime.NumCPU()
	maxWorkers := 1

	// 1コアはシステム用に残す
	if maxWorkers < 3 {
		maxWorkers = 1
	}

	fmt.Printf("Detected %d CPU cores, using %d workers\n", numCPU, maxWorkers)

	return &AudioAnalyzer{
		InputFile:          inputFile,
		OutputDir:          outputDir,
		MaxWorkers:         maxWorkers,
		SilenceThreshold:   "-30dB",
		SilenceDuration:    "5",
		MinSegmentDuration: 30.0,
		MaxSegmentDuration: 60.0,
	}
}

// DetectSilence uses ffmpeg to detect silence periods in audio
func (a *AudioAnalyzer) DetectSilence() ([]float64, error) {
	cmd := exec.Command("ffmpeg",
		"-i", a.InputFile,
		"-af", fmt.Sprintf("silencedetect=noise=%s:duration=%s", a.SilenceThreshold, a.SilenceDuration),
		"-f", "null", "-")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg silence detection failed: %v", err)
	}

	return a.parseSilenceOutput(string(output))
}

// parseSilenceOutput parses ffmpeg silence detection output
func (a *AudioAnalyzer) parseSilenceOutput(output string) ([]float64, error) {
	var silencePoints []float64
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		if strings.Contains(line, "silence_end") {
			parts := strings.Fields(line)
			for i, part := range parts {
				if part == "silence_end:" && i+1 < len(parts) {
					timeStr := strings.TrimSpace(parts[i+1])
					if time, err := strconv.ParseFloat(timeStr, 64); err == nil {
						silencePoints = append(silencePoints, time)
					}
				}
			}
		}
	}

	return silencePoints, nil
}

// GetAudioDuration gets the total duration of the audio file
func (a *AudioAnalyzer) GetAudioDuration() (float64, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		a.InputFile)

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get audio duration: %v", err)
	}

	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration: %v", err)
	}

	return duration, nil
}

// CreateSegments creates audio segments based on silence detection
func (a *AudioAnalyzer) CreateSegments() ([]AudioSegment, error) {
	// 無音区間を検出
	silencePoints, err := a.DetectSilence()
	if err != nil {
		return nil, err
	}

	// 音声の総時間を取得
	totalDuration, err := a.GetAudioDuration()
	if err != nil {
		return nil, err
	}

	// セグメントを作成
	var segments []AudioSegment
	currentStart := 0.0

	for _, silenceEnd := range silencePoints {
		if silenceEnd-currentStart >= 10 { // 最小3秒のセグメント
			segment := a.createSegment(currentStart, silenceEnd, len(segments))
			segments = append(segments, segment)
			currentStart = silenceEnd
		}
	}

	// 最後のセグメント
	if totalDuration-currentStart >= 10 {
		segment := a.createSegment(currentStart, totalDuration, len(segments))
		segments = append(segments, segment)
	}

	// 長すぎるセグメントを分割
	segments = a.splitLongSegments(segments)

	return segments, nil
}

// createSegment creates a single audio segment
func (a *AudioAnalyzer) createSegment(start, end float64, index int) AudioSegment {
	filename := fmt.Sprintf("segment_%03d_%.1f-%.1f.wav", index, start, end)
	filePath := filepath.Join(a.OutputDir, filename)

	return AudioSegment{
		StartTime: start,
		EndTime:   end,
		Filename:  filename,
		FilePath:  filePath,
		Duration:  end - start,
	}
}

// splitLongSegments splits segments that are too long
func (a *AudioAnalyzer) splitLongSegments(segments []AudioSegment) []AudioSegment {
	var result []AudioSegment

	for _, segment := range segments {
		if segment.Duration <= a.MaxSegmentDuration {
			result = append(result, segment)
		} else {
			// 長いセグメントを分割
			numSplits := int(segment.Duration/a.MaxSegmentDuration) + 1
			splitDuration := segment.Duration / float64(numSplits)

			for i := 0; i < numSplits; i++ {
				start := segment.StartTime + float64(i)*splitDuration
				end := segment.StartTime + float64(i+1)*splitDuration
				if i == numSplits-1 {
					end = segment.EndTime // 最後のセグメントは元の終了時間
				}

				splitSegment := a.createSegment(start, end, len(result))
				result = append(result, splitSegment)
			}
		}
	}

	return result
}

// ExtractAudioSegments extracts audio segments using ffmpeg
func (a *AudioAnalyzer) ExtractAudioSegments(segments []AudioSegment) error {
	// 出力ディレクトリを作成
	if err := os.MkdirAll(a.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	// 並行処理でセグメントを抽出
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, a.MaxWorkers)

	for _, segment := range segments {
		wg.Add(1)
		go func(seg AudioSegment) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := a.extractSingleSegment(seg); err != nil {
				log.Printf("Failed to extract segment %s: %v", seg.Filename, err)
			}
		}(segment)
	}

	wg.Wait()
	return nil
}

// extractSingleSegment extracts a single audio segment
func (a *AudioAnalyzer) extractSingleSegment(segment AudioSegment) error {
	cmd := exec.Command("ffmpeg",
		"-i", a.InputFile,
		"-ss", fmt.Sprintf("%.3f", segment.StartTime),
		"-t", fmt.Sprintf("%.3f", segment.Duration),
		"-c", "copy",
		"-y", // overwrite output file
		segment.FilePath)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg extraction failed: %v", err)
	}

	return nil
}

// ProcessWithCpp processes audio segments with Python Whisper in parallel
func (a *AudioAnalyzer) ProcessWithCpp(segments []AudioSegment) ([]TranscriptionResult, error) {
	results := make([]TranscriptionResult, len(segments))
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, a.MaxWorkers)

	for i, segment := range segments {
		wg.Add(1)
		go func(index int, seg AudioSegment) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			text, err := a.transcribeCpp(seg)
			result := TranscriptionResult{
				Segment: seg,
			}

			if err != nil {
				result.Error = err.Error()
			} else {
				result.Text = text
			}

			results[index] = result
		}(i, segment)
	}

	wg.Wait()

	// 時間順にソート
	sort.Slice(results, func(i, j int) bool {
		return results[i].Segment.StartTime < results[j].Segment.StartTime
	})

	return results, nil
}

func (a *AudioAnalyzer) transcribeCpp(segment AudioSegment) (string, error) {
	// PythonスクリプトにJSONで音声ファイル情報を渡す
	filepath := segment.FilePath
	fmt.Println("Transcribing segment:", "./"+strings.ReplaceAll(filepath, "\\", "/"))
	exePath := "./whisper.cpp/build/bin/Release/main.exe"
	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		log.Fatal("main.exe が見つかりません")
	}

	seconds, err := GetAudioDuration(filepath)
	fmt.Printf("音声の長さ: %.2f 秒\n", seconds)

	secondsStr := fmt.Sprintf("%.0f", seconds*1000) // convert seconds to milliseconds and string

	initialPrompt := `この音声は大学のサークル活動に関する会話です。
					内容には「理科大（りかだい）」または「理大（りだい）」という大学名が登場します。
					また、「理大祭（りだいさい）」というイベント名が含まれる場合があります。
					会話は自然な日本語で行われており、学生同士のカジュアルなやり取りが含まれます。
					固有名詞（大学名・イベント名など）は正確に認識してください。
					日本語の音声の認識を行います`

	cmd := exec.Command("./whisper.cpp/build/bin/Release/whisper-cli.exe", "-m", "./whisper.cpp/models/ggml-large-v3.bin", "-f", "./"+strings.ReplaceAll(filepath, "\\", "/"), "-l", "ja", "--vad-min-speech-duration-ms", secondsStr, "-otxt", "-tdrz", "-sns", "--suppress-nst", "--prompt", initialPrompt)
	output, err := cmd.CombinedOutput()
	fmt.Printf("C++ whisper output: %s\n", string(output))
	if err != nil {
		return "", fmt.Errorf("C++ whisper transcription failed: %v\n%s", err, string(output))
	}
	result := strings.TrimSpace(string(output))

	return result, nil
}

// Run executes the complete audio analysis pipeline
func (a *AudioAnalyzer) Run() error {
	fmt.Println("Starting audio analysis...")

	// Step 1: Create segments
	fmt.Println("Creating audio segments...")
	segments, err := a.CreateSegments()
	if err != nil {
		return fmt.Errorf("failed to create segments: %v", err)
	}
	fmt.Printf("Created %d segments\n", len(segments))

	// Step 2: Extract audio segments
	fmt.Println("Extracting audio segments...")
	if err := a.ExtractAudioSegments(segments); err != nil {
		return fmt.Errorf("failed to extract segments: %v", err)
	}

	// Step 3: Process with Python Whisper
	fmt.Println("Processing with C++ Whisper...")
	results, err := a.ProcessWithCpp(segments)
	if err != nil {
		return fmt.Errorf("failed to process with C++: %v", err)
	}

	// Step 4: Output results
	if err := a.OutputResults(results); err != nil {
		return fmt.Errorf("failed to output results: %v", err)
	}

	fmt.Println("Audio analysis completed successfully!")
	return nil
}

// OutputResults outputs the transcription results
func (a *AudioAnalyzer) OutputResults(results []TranscriptionResult) error {
	// JSON出力
	jsonFile := filepath.Join(a.OutputDir, "transcription_results.json")
	jsonData, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %v", err)
	}

	if err := os.WriteFile(jsonFile, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write JSON file: %v", err)
	}

	// テキスト出力
	txtFile := filepath.Join(a.OutputDir, "transcription_results.txt")
	var textBuilder strings.Builder
	textBuilder.WriteString("\uFEFF") // UTF-8 BOM
	for _, result := range results {
		if result.Error != "" {
			textBuilder.WriteString(fmt.Sprintf("[%.1fs-%.1fs] ERROR: %s\n",
				result.Segment.StartTime, result.Segment.EndTime, result.Error))
		} else {
			textBuilder.WriteString(fmt.Sprintf("[%.1fs-%.1fs] %s\n",
				result.Segment.StartTime, result.Segment.EndTime, result.Text))
		}
	}

	if err := os.WriteFile(txtFile, []byte(textBuilder.String()), 0644); err != nil {
		return fmt.Errorf("failed to write text file: %v", err)
	}

	// コンソール出力
	fmt.Println("\n=== Transcription Results ===")
	for _, result := range results {
		if result.Error != "" {
			fmt.Printf("[%.1fs-%.1fs] ERROR: %s\n",
				result.Segment.StartTime, result.Segment.EndTime, result.Error)
		} else {
			fmt.Printf(result.Text)
		}
	}

	return nil
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run main.go <input_audio_file> <output_directory> [max_workers] [python_script]")
		fmt.Println("  max_workers: optional, number of parallel workers (default: auto-detect)")
		fmt.Println("  python_script: optional, path to Python whisper script (default: ./whisper_transcriber.py)")
		os.Exit(1)
	}

	inputFile := os.Args[1]
	outputDir := os.Args[2]
	// maxWorkerに
	analyzer := NewAudioAnalyzer(inputFile, outputDir)

	if err := analyzer.Run(); err != nil {
		log.Fatalf("Audio analysis failed: %v", err)
	}
}

func GetAudioDuration(filePath string) (float64, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe failed: %v", err)
	}

	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration: %v", err)
	}

	return duration, nil
}
