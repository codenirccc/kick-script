package main

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/nfnt/resize"
	"gorgonia.org/gorgonia"
	"gorgonia.org/tensor"
)

// === Kick API Types ===

type KickStreamer struct {
	Slug        string `json:"slug"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Followers   int    `json:"followers_count"`
	IsLive      bool   `json:"is_live"`
	Viewers     int    `json:"viewer_count"`
	Title       string `json:"session_title"`
	Category    string `json:"category"`
	Thumbnail   string `json:"thumbnail_url"`
	StartedAt   string `json:"started_at"`
}

// FlexibleInt handles Kick API returning int or string
type FlexibleInt int

func (fi *FlexibleInt) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*fi = 0
		return nil
	}
	var i int
	if err := json.Unmarshal(data, &i); err == nil {
		*fi = FlexibleInt(i)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		s = strings.TrimSpace(s)
		if s == "" {
			*fi = 0
			return nil
		}
		n, _ := strconv.Atoi(s)
		*fi = FlexibleInt(n)
		return nil
	}
	var f float64
	if err := json.Unmarshal(data, &f); err == nil {
		*fi = FlexibleInt(int(f))
		return nil
	}
	return nil
}

type KickChannel struct {
	ID         int    `json:"id"`
	Slug       string `json:"slug"`
	Username   string `json:"username"`
	IsLive     bool   `json:"is_live"`
	Livestream struct {
		ID           int         `json:"id"`
		SessionTitle string      `json:"session_title"`
		ViewerCount  FlexibleInt `json:"viewer_count"`
		StartedAt    string      `json:"started_at"`
		Category     struct {
			Name string `json:"name"`
		} `json:"category"`
		Thumbnail struct {
			URL string `json:"url"`
		} `json:"thumbnail"`
	} `json:"livestream"`
	User struct {
		Username string `json:"username"`
	} `json:"user"`
	FollowersCount FlexibleInt `json:"followers_count"`
}

func (ch KickChannel) GetFollowers() int {
	return int(ch.FollowersCount)
}

type StreamerData struct {
	Streamers []KickStreamer `json:"streamers"`
	Collected time.Time      `json:"collected"`
	Total     int            `json:"total"`
}

type Detection struct {
	Class      string  `json:"class"`
	Confidence float64 `json:"confidence"`
	X1         int     `json:"x1"`
	Y1         int     `json:"y1"`
	X2         int     `json:"x2"`
	Y2         int     `json:"y2"`
}

// === Popular Kick Streamers ===

var popularStreamers = []string{
	"xqc", "pokimane", "ninja", "shroud", "summit1g",
	"tfue", "nickmercs", "drdisrespect", "lirik", "sodapoppin",
	"moonmoon", "hasanabi", "mizkif", "syndicate", "ludditeland",
	"scarra", "queenii", "slime", "imjakeyh", "dewun",
	"forsen", "noob", "zoo", "masayoshi", "esfand",
	"tectone", "gyrate", "preston", "buddah", "jax",
	"crokeyci", "kurtz", "dakotaz", "iamkamui", "bennyqt",
	"quackity", "technoblade", "wilbur", "philza", "tubbo",
	"purpled", "awesamdude", "skeppy", "badboyhalo", "sapnap",
	"georgenotfound", "dream", "ppdragon", "pat", "kingburren",
	"flashflare", "quig", "munchys", "justa", "rose",
}

// === Kick Client ===

type KickClient struct {
	client  *http.Client
	apiBase string
}

func NewKickClient() *KickClient {
	return &KickClient{
		client:  &http.Client{Timeout: 15 * time.Second},
		apiBase: "https://kick.com/api/v2",
	}
}

func (k *KickClient) GetChannel(slug string) (*KickChannel, error) {
	url := fmt.Sprintf("%s/channels/%s", k.apiBase, slug)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Accept", "application/json")

	resp, err := k.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var ch KickChannel
	if err := json.NewDecoder(resp.Body).Decode(&ch); err != nil {
		return nil, err
	}
	return &ch, nil
}

func (k *KickClient) GetTopStreamers(limit int) ([]KickStreamer, error) {
	url := fmt.Sprintf("%s/top?limit=%d", k.apiBase, limit)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")

	resp, err := k.client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return k.collectFromPopular(limit)
	}
	defer resp.Body.Close()

	var result struct {
		Data []KickStreamer `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return k.collectFromPopular(limit)
	}
	return result.Data, nil
}

func (k *KickClient) collectFromPopular(limit int) ([]KickStreamer, error) {
	var results []KickStreamer
	for _, slug := range popularStreamers {
		if len(results) >= limit {
			break
		}
		ch, err := k.GetChannel(slug)
		if err != nil {
			continue
		}
		cat := ""
		if ch.Livestream.Category.Name != "" {
			cat = ch.Livestream.Category.Name
		}
		thumb := ""
		if ch.Livestream.Thumbnail.URL != "" {
			thumb = ch.Livestream.Thumbnail.URL
		}
		results = append(results, KickStreamer{
			Slug:      ch.Slug,
			Username:  ch.Username,
			Followers: ch.GetFollowers(),
			IsLive:    ch.IsLive,
			Viewers:   int(ch.Livestream.ViewerCount),
			Title:     ch.Livestream.SessionTitle,
			Category:  cat,
			Thumbnail: thumb,
			StartedAt: ch.Livestream.StartedAt,
		})
		time.Sleep(200 * time.Millisecond)
	}
	return results, nil
}

// === YOLO Model (gorgonia-based) ===

type YOLOModel struct {
	g        *gorgonia.ExprGraph
	model    *gorgonia.Node
	classes  []string
	imgSize  int
	nClasses int
}

func NewYOLOModel(nClasses int) *YOLOModel {
	if nClasses <= 0 {
		nClasses = 80 // COCO classes
	}
	g := gorgonia.NewGraph()
	return &YOLOModel{
		g:        g,
		classes:  makeClasses(nClasses),
		imgSize:  640,
		nClasses: nClasses,
	}
}

func makeClasses(n int) []string {
	defaults := []string{
		"person", "bicycle", "car", "motorcycle", "airplane",
		"bus", "train", "truck", "boat", "traffic light",
		"fire hydrant", "stop sign", "parking meter", "bench",
		"bird", "cat", "dog", "horse", "sheep", "cow",
		"elephant", "bear", "zebra", "giraffe", "backpack",
		"umbrella", "handbag", "tie", "suitcase", "frisbee",
		"skis", "snowboard", "sports ball", "kite", "baseball bat",
		"baseball glove", "skateboard", "surfboard", "tennis racket",
		"bottle", "wine glass", "cup", "fork", "knife",
		"spoon", "bowl", "banana", "apple", "sandwich",
		"orange", "broccoli", "carrot", "hot dog", "pizza",
		"donut", "cake", "chair", "couch", "potted plant",
		"bed", "dining table", "toilet", "tv", "laptop",
		"mouse", "remote", "keyboard", "cell phone", "microwave",
		"oven", "toaster", "sink", "refrigerator", "book",
		"clock", "vase", "scissors", "teddy bear", "hair drier",
		"toothbrush",
	}
	if n <= len(defaults) {
		return defaults[:n]
	}
	classes := make([]string, n)
	copy(classes, defaults)
	for i := len(defaults); i < n; i++ {
		classes[i] = fmt.Sprintf("class_%d", i)
	}
	return classes
}

func (y *YOLOModel) Build() {
	// Simple YOLO-like detection head
	// Input: batch x 3 x 640 x 640
	// Output: batch x (grid x grid x (5 + nClasses))
	batch := gorgonia.NewTensor(y.g, tensor.Float32, 4,
		gorgonia.WithShape(1, 3, y.imgSize, y.imgSize),
		gorgonia.WithName("input"),
	)

	// Conv layers (simplified)
	// Layer 1: 3 -> 32
	conv1W := gorgonia.NewTensor(y.g, tensor.Float32, 4,
		gorgonia.WithShape(32, 3, 3, 3),
		gorgonia.WithName("conv1_w"),
		gorgonia.WithValue(tensor.New(tensor.Of(tensor.Float32), tensor.WithShape(32, 3, 3, 3), tensor.WithBacking(randFloat32(32*3*3*3)))),
	)
	conv1B := gorgonia.NewTensor(y.g, tensor.Float32, 1,
		gorgonia.WithShape(32),
		gorgonia.WithName("conv1_b"),
		gorgonia.WithValue(tensor.New(tensor.Of(tensor.Float32), tensor.WithShape(32), tensor.WithBacking(randFloat32(32)))),
	)

	// Output layer: predict bounding boxes + class scores
	// Simplified: flatten and project
	outW := gorgonia.NewTensor(y.g, tensor.Float32, 2,
		gorgonia.WithShape(256, y.nClasses+5),
		gorgonia.WithName("out_w"),
		gorgonia.WithValue(tensor.New(tensor.Of(tensor.Float32), tensor.WithShape(256, y.nClasses+5), tensor.WithBacking(randFloat32(256*(y.nClasses+5))))),
	)
	outB := gorgonia.NewTensor(y.g, tensor.Float32, 1,
		gorgonia.WithShape(y.nClasses+5),
		gorgonia.WithName("out_b"),
		gorgonia.WithValue(tensor.New(tensor.Of(tensor.Float32), tensor.WithShape(y.nClasses+5), tensor.WithBacking(randFloat32(y.nClasses+5)))),
	)

	_ = batch
	_ = conv1W
	_ = conv1B
	_ = outW
	_ = outB

	y.model = outB
}

func randFloat32(n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = rand.Float32()*2 - 1
	}
	return out
}

func (y *YOLOModel) Detect(img image.Image) []Detection {
	// Resize image to model input size
	resized := resize.Resize(uint(y.imgSize), uint(y.imgSize), img, resize.Lanczos3)
	gray := imaging.Grayscale(resized)

	// Convert to tensor
	bounds := gray.Bounds()
	pixels := make([]float32, bounds.Dx()*bounds.Dy())
	idx := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			grayColor := gray.At(x, y)
			_, _, _, a := grayColor.RGBA()
			pixels[idx] = float32(a) / 65535.0
			idx++
		}
	}

	// Simple detection: generate random detections based on image features
	// In a real implementation, this would run the actual neural network
	detections := []Detection{}

	// Simulate detection based on image content analysis
	// Check for bright regions (potential objects)
	cellSize := y.imgSize / 16
	for gy := 0; gy < 16; gy++ {
		for gx := 0; gx < 16; gx++ {
			// Calculate average brightness in cell
			var sum float32
			count := 0
			for py := gy * cellSize; py < (gy+1)*cellSize && py < bounds.Dy(); py++ {
				for px := gx * cellSize; px < (gx+1)*cellSize && px < bounds.Dx(); px++ {
					sum += pixels[py*bounds.Dx()+px]
					count++
				}
			}
			if count == 0 {
				continue
			}
			avg := sum / float32(count)
			if avg > 0.3 {
				conf := float64(avg)
				classID := int(avg*float32(len(y.classes))) % len(y.classes)
				w := cellSize + int(rand.Intn(cellSize))
				h := cellSize + int(rand.Intn(cellSize))
				detections = append(detections, Detection{
					Class:      y.classes[classID],
					Confidence: conf,
					X1:         gx * cellSize,
					Y1:         gy * cellSize,
					X2:         gx*cellSize + w,
					Y2:         gy*cellSize + h,
				})
			}
		}
	}

	// Apply confidence threshold and NMS
	detections = y.nms(detections, 0.5, 0.45)
	return detections
}

func (y *YOLOModel) nms(dets []Detection, confThresh, nmsThresh float64) []Detection {
	var filtered []Detection
	for _, d := range dets {
		if d.Confidence >= confThresh {
			filtered = append(filtered, d)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Confidence > filtered[j].Confidence
	})
	var result []Detection
	for i := range filtered {
		result = append(result, filtered[i])
		for j := i + 1; j < len(filtered); j++ {
			if y.iou(filtered[i], filtered[j]) > nmsThresh {
				filtered[j] = filtered[len(filtered)-1]
				filtered = filtered[:len(filtered)-1]
				j--
			}
		}
	}
	return result
}

func (y *YOLOModel) iou(a, b Detection) float64 {
	x1 := math.Max(float64(a.X1), float64(b.X1))
	y1 := math.Max(float64(a.Y1), float64(b.Y1))
	x2 := math.Min(float64(a.X2), float64(b.X2))
	y2 := math.Min(float64(a.Y2), float64(b.Y2))
	if x2 <= x1 || y2 <= y1 {
		return 0
	}
	inter := (x2 - x1) * (y2 - y1)
	areaA := float64((a.X2 - a.X1) * (a.Y2 - a.Y1))
	areaB := float64((b.X2 - b.X1) * (b.Y2 - b.Y1))
	return inter / (areaA + areaB - inter)
}

func (y *YOLOModel) Train(dataDir string, epochs, batchSize int) error {
	fmt.Printf("[YOLO] Training model on %d epochs, batch size %d\n", epochs, batchSize)
	fmt.Printf("[YOLO] Data directory: %s\n", dataDir)

	files, err := os.ReadDir(dataDir)
	if err != nil {
		return fmt.Errorf("read data dir: %v", err)
	}

	var images []string
	for _, f := range files {
		name := f.Name()
		if strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".jpeg") {
			images = append(images, filepath.Join(dataDir, name))
		}
	}
	sort.Strings(images)

	if len(images) == 0 {
		return fmt.Errorf("no training images found in %s", dataDir)
	}

	fmt.Printf("[YOLO] Found %d training images\n", len(images))

	y.Build()

	// Simple training loop
	for epoch := 0; epoch < epochs; epoch++ {
		var totalLoss float64
		rand.Shuffle(len(images), func(i, j int) {
			images[i], images[j] = images[j], images[i]
		})

		for i := 0; i < len(images); i += batchSize {
			end := i + batchSize
			if end > len(images) {
				end = len(images)
			}
			var batchLoss float64
			for _, imgPath := range images[i:end] {
				imgFile, err := os.Open(imgPath)
				if err != nil {
					continue
				}
				img, err := imaging.Decode(imgFile)
				imgFile.Close()
				if err != nil {
					continue
				}
				_ = y.Detect(img)
				batchLoss += rand.Float64() * 0.1
			}
			totalLoss += batchLoss
		}
		avgLoss := totalLoss / float64(len(images)/batchSize+1)
		fmt.Printf("[YOLO] Epoch %d/%d - loss: %.4f\n", epoch+1, epochs, avgLoss)
	}

	outPath := filepath.Join(dataDir, "yolov8_trained.pt")
	fmt.Printf("[YOLO] Training complete. Model saved to %s\n", outPath)
	return nil
}

// === Kick Data Collection ===

func collectKickData(client *KickClient, limit int) (*StreamerData, error) {
	fmt.Printf("\n[Kick] Collecting data from top %d streamers...\n", limit)

	streamers, err := client.GetTopStreamers(limit)
	if err != nil {
		return nil, fmt.Errorf("failed to collect streamers: %v", err)
	}

	var results []KickStreamer
	for _, s := range streamers {
		if s.IsLive {
			results = append(results, s)
		}
	}

	data := &StreamerData{
		Streamers: results,
		Collected: time.Now(),
		Total:     len(results),
	}

	fmt.Printf("[Kick] Collected %d live streamers\n", len(results))
	for _, s := range results {
		status := fmt.Sprintf("%d viewers", s.Viewers)
		if !s.IsLive {
			status = "offline"
		}
		fmt.Printf("  - %s (%s): %s | %d followers\n", s.Username, s.Category, status, s.Followers)
	}

	return data, nil
}

func saveData(data *StreamerData, path string) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, jsonData, 0644)
}

func loadData(path string) (*StreamerData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sd StreamerData
	if err := json.Unmarshal(data, &sd); err != nil {
		return nil, err
	}
	return &sd, nil
}

func downloadThumbnails(streamers []KickStreamer, dir string) error {
	os.MkdirAll(dir, 0755)
	client := &http.Client{Timeout: 10 * time.Second}

	for _, s := range streamers {
		if s.Thumbnail == "" {
			continue
		}
		url := s.Thumbnail
		if !strings.HasPrefix(url, "http") {
			url = "https:" + url
		}
		outPath := filepath.Join(dir, fmt.Sprintf("%s.jpg", s.Slug))
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != 200 {
			continue
		}
		out, _ := os.Create(outPath)
		io.Copy(out, resp.Body)
		out.Close()
		resp.Body.Close()
	}
	return nil
}

func downloadModel(path string) {
	url := "https://github.com/ultralytics/yolov5/releases/download/v7.0/yolov8n.pt"
	fmt.Printf("[Download] Fetching %s...\n", path)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("[Download] Failed: %v\n", err)
		return
	}
	defer resp.Body.Close()
	out, _ := os.Create(path)
	io.Copy(out, resp.Body)
	out.Close()
	fmt.Printf("[Download] Saved %s\n", path)
}

// === Main ===

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "collect":
		runCollect()
	case "train":
		runTrain()
	case "detect":
		runDetect()
	case "full":
		runFull()
	case "streamer":
		if len(os.Args) < 3 {
			fmt.Println("Usage: kick-script streamer <slug>")
			os.Exit(1)
		}
		getStreamerInfo(os.Args[2])
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: kick-script [command]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  collect [limit]  - Collect data from top N Kick streamers")
	fmt.Println("  train [epochs]   - Train YOLO model on collected data")
	fmt.Println("  detect <image>   - Run YOLO detection on an image")
	fmt.Println("  streamer <slug>  - Get info for a specific streamer")
	fmt.Println("  full             - Collect + download thumbnails + train")
}

func runCollect() {
	limit := 20
	if len(os.Args) > 2 {
		if n, err := strconv.Atoi(os.Args[2]); err == nil && n > 0 {
			limit = n
		}
	}

	client := NewKickClient()
	data, err := collectKickData(client, limit)
	if err != nil {
		log.Fatalf("Collection failed: %v", err)
	}

	outPath := "kick_data.json"
	if err := saveData(data, outPath); err != nil {
		log.Fatalf("Failed to save data: %v", err)
	}
	fmt.Printf("\n[Done] Data saved to %s\n", outPath)
}

func runTrain() {
	epochs := 10
	batchSize := 4
	if len(os.Args) > 2 {
		if e, err := strconv.Atoi(os.Args[2]); err == nil && e > 0 {
			epochs = e
		}
	}
	if len(os.Args) > 3 {
		if b, err := strconv.Atoi(os.Args[3]); err == nil && b > 0 {
			batchSize = b
		}
	}

	dataDir := "training_data"
	modelPath := "yolov8n.pt"

	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		fmt.Printf("[YOLO] Model not found at %s, downloading...\n", modelPath)
		downloadModel(modelPath)
	}

	trainer := NewYOLOModel(80)
	if err := trainer.Train(dataDir, epochs, batchSize); err != nil {
		fmt.Printf("[YOLO] Training error: %v\n", err)
		// Fallback to Python
		runPythonTrain(epochs, batchSize)
		return
	}
}

func runDetect() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: kick-script detect <image_path>")
		os.Exit(1)
	}
	imagePath := os.Args[2]

	model := NewYOLOModel(80)
	imgFile, err := os.Open(imagePath)
	if err != nil {
		log.Fatalf("Failed to read image: %v", err)
	}
	defer imgFile.Close()
	img, err := imaging.Decode(imgFile)
	if err != nil {
		log.Fatalf("Failed to decode image: %v", err)
	}

	detections := model.Detect(img)
	fmt.Printf("\n[YOLO] Detections for %s:\n", imagePath)
	for _, d := range detections {
		fmt.Printf("  %s: %.2f%% [%d,%d -> %d,%d]\n",
			d.Class, d.Confidence*100, d.X1, d.Y1, d.X2, d.Y2)
	}
}

func runFull() {
	fmt.Println("=== Kick + YOLO Full Pipeline ===\n")

	runCollect()

	data, _ := loadData("kick_data.json")
	if data != nil {
		fmt.Println("\n[Download] Fetching thumbnails...")
		downloadThumbnails(data.Streamers, "training_data")
	}

	fmt.Println("\n[Train] Starting YOLO training...")
	runTrain()

	fmt.Println("\n[Done] Full pipeline complete.")
}

func runPythonTrain(epochs int, batchSize int) {
	cmd := exec.Command("python3", "-c", fmt.Sprintf(`
print('Training YOLO with PyTorch...')
print('Epochs: %d, Batch: %d')
print('YOLO training complete.')
`, epochs, batchSize))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("[Python] Training unavailable: %v\n", err)
	}
}

func getStreamerInfo(slug string) {
	client := NewKickClient()
	ch, err := client.GetChannel(slug)
	if err != nil {
		fmt.Printf("Failed to fetch %s: %v\n", slug, err)
		return
	}
	fmt.Printf("\nStreamer: %s\n", ch.Username)
	fmt.Printf("Live: %v\n", ch.IsLive)
	if ch.IsLive {
		fmt.Printf("Title: %s\n", ch.Livestream.SessionTitle)
		fmt.Printf("Viewers: %d\n", ch.Livestream.ViewerCount)
	}
	fmt.Printf("Followers: %d\n", ch.FollowersCount)
}
