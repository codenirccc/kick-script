# kick-script

Go script that collects data from popular Kick.com streamers and trains YOLO models.

## Commands

```
kick-script collect [limit]    - Collect data from top N Kick streamers
kick-script train [epochs]     - Train YOLO model on collected data
kick-script detect <image>     - Run YOLO detection on an image
kick-script streamer <slug>    - Get info for a specific streamer
kick-script full               - Collect + download thumbnails + train
```

## Quick Start

```powershell
# Build
$env:ASSUME_NO_MOVING_GC_UNSAFE_RISK_IT_WITH="go1.27"
go build -o kick.exe .

# Collect data from top 20 streamers
.\kick.exe collect 20

# Get specific streamer info
.\kick.exe streamer xqc

# Train YOLO on collected thumbnails
.\kick.exe train 10

# Detect objects in an image
.\kick.exe detect test.jpg

# Full pipeline
.\kick.exe full
```

## Features

- **Kick API**: Fetches live streamer data (username, viewers, followers, category, thumbnail)
- **Popular Streamers**: Built-in list of 50 popular Kick streamers
- **YOLOv8**: Object detection and training using gorgonia (pure Go)
- **Thumbnail Download**: Automatically fetches streamer thumbnails for training
- **NMS**: Non-maximum suppression for detection filtering

## Dependencies

- Go 1.27+
- `gorgonia.org/gorgonia` - Neural network operations
- `github.com/nfnt/resize` - Image resizing
- `github.com/disintegration/imaging` - Image processing

## Notes

- Set `ASSUME_NO_MOVING_GC_UNSAFE_RISK_IT_WITH=go1.27` when running (gorgonia dependency)
- Kick API may require authentication for some endpoints
- YOLO training uses a simplified Go implementation; for full training, use PyTorch
