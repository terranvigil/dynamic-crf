# dynamic-crf

A CLI tool for finding the optimal CRF (Constant Rate Factor) value for video encoding by targeting a specific [VMAF](https://github.com/Netflix/vmaf) quality score. Rather than brute-force encoding at many bitrates, `dynamic-crf` uses a hybrid bisection/interpolation search with efficient VMAF sampling to converge quickly on the CRF that achieves your target quality.

Inspired by Netflix's per-title encoding optimization and Jan Ozer's paper [Formulate the Optimal Encoding Ladder with VMAF](https://streaminglearningcenter.com/encoding/optimal_encoding_ladder_vmaf.html).

## How it works

1. Select a target VMAF score (e.g., 95) and a CRF search range.
2. For videos longer than 60 seconds, generate a representative sample from detected scene changes. Falls back to uniform temporal sampling when scene detection yields too few results.
3. Score the CRF range boundaries to establish the VMAF envelope.
4. Search using a hybrid algorithm: 70% bisection (guarantees convergence by halving the range) blended with 30% interpolation (biases toward the expected position). This handles VMAF's non-linear relationship with CRF, which is logarithmic/sigmoidal rather than linear.
5. Converge when the VMAF score is within tolerance of the target, or after a maximum of 20 iterations.

### VMAF scoring guidelines

| Target | Quality |
|--------|---------|
| 93 | Good — suitable for most content |
| 95 | Near-indistinguishable from the reference |
| 90+ | Should look good to most viewers |

Three VMAF model types are typically available: 4K, HD, and Phone.

## Prerequisites

The following tools must be installed and available on your `PATH`:

- [Go](https://go.dev/dl/) 1.22+
- [FFmpeg](https://ffmpeg.org/download.html) (with libvmaf support)
- [ffprobe](https://ffmpeg.org/ffprobe.html) (included with FFmpeg)
- [MediaInfo](https://mediaarea.net/en/MediaInfo/Download) (CLI version)
- [vmaf](https://github.com/Netflix/vmaf) (CLI, only required for the `cambi` action)

Verify your setup:

```bash
ffmpeg -version | head -1
ffprobe -version | head -1
mediainfo --Version
```

## Installation

```bash
# from source
git clone https://github.com/terranvigil/dynamic-crf.git
cd dynamic-crf
make build

# the binary is written to ./dynamic-crf
./dynamic-crf -h
```

## Usage

```
dynamic-crf -a <action> -i <input> [options]
```

### Actions

| Action | Description | Requires `-o` |
|--------|-------------|:---:|
| `optimize` | Search for optimal CRF, encode, and score the result | Yes |
| `search` | Find the optimal CRF for a target VMAF (no output file) | No |
| `encode` | Encode with a given CRF or bitrate, then score | Yes |
| `inspect` | Write source metadata as JSON to `{source}_inspect.json` | No |
| `vmaf` | Calculate VMAF between source (reference) and output (distorted) | Yes |
| `cambi` | Calculate CAMBI banding artifact scores | Yes |

### Flags

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `-action` | `-a` | | Action to perform (required) |
| `-input` | `-i` | | Path to input/source file (required) |
| `-output` | `-o` | | Path to output file (`.mp4`). Required for all actions except `search` and `inspect` |
| `-targetvmaf` | | `95.0` | Target VMAF score (0-100) |
| `-tolerance` | | `0.5` | VMAF score tolerance for search convergence |
| `-initialcrf` | | `20` | Starting CRF value for search |
| `-mincrf` | | `30` | Low-quality CRF bound (higher number = lower quality) |
| `-maxcrf` | | `15` | High-quality CRF bound (lower number = higher quality) |
| `-codec` | | `libx264` | Video codec |
| `-height` | `-h` | | Output height (preserves aspect ratio) |
| `-width` | `-w` | | Output width (preserves aspect ratio) |
| `-maxbitrate` | `-mb` | | Peak bitrate limit (kbps) |
| `-buffersize` | `-bs` | | HRD buffer size (kbps) |
| `-bitrate` | | | Target bitrate for `encode` action (kbps) |
| `-crf` | | | CRF value for `encode` action |
| `-tune` | `-t` | | Encoder tune: `animation`, `film`, `grain`, `psnr`, `ssim` |
| `-minbitrate` | | | Minimum bitrate (forces CBR — not recommended) |

### Examples

```bash
# find optimal CRF and produce an optimized encode
dynamic-crf -a optimize -i source.mp4 -o optimized.mp4 -h 1080 -mb 12000 -bs 48000

# search for the best CRF without producing an output file
dynamic-crf -a search -i source.mp4 -h 720 -mb 6000 -bs 24000 -t animation

# encode at a specific CRF and report its VMAF
dynamic-crf -a encode -i source.mp4 -o output.mp4 -crf 18 -h 720

# score an existing encode against its source
dynamic-crf -a vmaf -i source.mp4 -o encoded.mp4

# check for banding artifacts
dynamic-crf -a cambi -i source.mp4 -o encoded.mp4

# inspect source metadata
dynamic-crf -a inspect -i source.mp4
```

## Project structure

```
cmd/                        CLI entry point
actions/                    High-level orchestration (search, optimize, score)
commands/                   FFmpeg/ffprobe/mediainfo wrappers
inspect/                    Scene detection and sampling
model/                      Data structures for FFprobe and MediaInfo output
testutil/                   Test helpers and fixture loading
fixtures/                   Test data
```

## Development

```bash
make build          # compile binary
make test           # run unit tests (race detector enabled)
make fmt            # format with gofumpt
make lint           # run golangci-lint
```

### Running tests

```bash
# unit tests
make test

# integration tests (requires media fixtures)
go test -v -race -count=1 -tags=integration ./...
```

## Roadmap

- [ ] Per-scene variable CRF optimization
- [ ] Parallel encode/score operations
- [ ] Scene transition detection (fades, dissolves)
- [ ] VMAF model training for anime content
- [ ] HD/TV and phone resolution model training

## References

### Research and papers

- [Formulate the Optimal Encoding Ladder with VMAF](https://streaminglearningcenter.com/encoding/optimal_encoding_ladder_vmaf.html) — Jan Ozer
- [Dynamic Optimizer: A Perceptual Video Encoding Optimization Framework](https://netflixtechblog.com/dynamic-optimizer-a-perceptual-video-encoding-optimization-framework-e19f1e3a277f) — Netflix
- [Toward a Practical Perceptual Video Quality Metric](https://netflixtechblog.com/toward-a-practical-perceptual-video-quality-metric-653f208b9652) — Netflix
- [VMAF: The Journey Continues](https://netflixtechblog.com/vmaf-the-journey-continues-44b51ee9ed12) — Netflix
- [CAMBI: A Banding Artifact Detector](https://netflixtechblog.com/cambi-a-banding-artifact-detector-96777ae12fe2) — Netflix
- [Finding the Just Noticeable Difference with VMAF](https://streaminglearningcenter.com/codecs/finding-the-just-noticeable-difference-with-netflix-vmaf.html)
- [A Practical Guide for VMAF](https://jina-liu.medium.com/a-practical-guide-for-vmaf-481b4d420d9c)
- [CRF Guide](https://slhck.info/video/2017/02/24/crf-guide.html) — Werner Robitza
- [Instant Per-Title Encoding](https://www.mux.com/blog/instant-per-title-encoding) — Mux

### Tools and documentation

- [Netflix VMAF](https://github.com/Netflix/vmaf)
- [FFmpeg libvmaf filter](https://ffmpeg.org/ffmpeg-filters.html#libvmaf)
- [FFmpeg VMAF usage](https://github.com/Netflix/vmaf/blob/master/resource/doc/ffmpeg.md)

### Test sources for training models

- [4K Media](https://4kmedia.org/)
- [Xiph.org Video Test Media](https://media.xiph.org/video/)

## License

[MIT](LICENSE)
