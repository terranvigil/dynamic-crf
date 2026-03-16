# dynamic-crf

Find the CRF that hits your target VMAF, without brute-force trial encodes.

A CLI tool for finding the optimal CRF (Constant Rate Factor) value for video encoding by targeting a specific [VMAF](https://github.com/Netflix/vmaf) quality score. Rather than encoding at many bitrates and scoring each one, `dynamic-crf` uses a hybrid bisection/interpolation search with efficient VMAF sampling to converge quickly on the CRF that achieves your target quality.

## The problem

When encoding video with CRF, you're choosing a quality level, but the relationship between CRF and perceived quality depends entirely on the content. A talking-head at CRF 23 might score a VMAF of 97. The same CRF on an action sequence might drop to 85. The only way to know is to encode, score, and try again.

The naive approach (encode at many CRF values, VMAF-score each one, pick the best) works but is expensive. Each VMAF pass alone can take longer than the encode. For a per-title encoding pipeline producing multiple renditions, the cost multiplies fast.

## The approach

This project was inspired by Netflix's [per-title encoding optimization](https://netflixtechblog.com/dynamic-optimizer-a-perceptual-video-encoding-optimization-framework-e19f1e3a277f) and Jan Ozer's paper [Formulate the Optimal Encoding Ladder with VMAF](https://streaminglearningcenter.com/encoding/optimal_encoding_ladder_vmaf.html). The key insight: VMAF scoring is the bottleneck, not encoding. Netflix's own [work on frame subsampling](https://netflixtechblog.com/vmaf-the-journey-continues-44b51ee9ed12) showed that trading off some scoring accuracy, particularly early in the search, can dramatically reduce total computation time.

`dynamic-crf` reduces the cost in two ways:

**Fewer encodes.** Instead of linear search, it uses a hybrid bisection/interpolation algorithm. Bisection guarantees convergence by halving the search range each step. Interpolation biases each guess toward where the target score is likely to land. The blend (70/30) handles the fact that VMAF's relationship with CRF is sigmoidal: nearly flat at the extremes and steep in the middle, where pure interpolation would overshoot.

**Cheaper scoring.** Rather than VMAF-scoring the entire video at each step, it scores a representative sample: up to 15 scenes (2-10 seconds each) selected from detected scene changes. For videos over 60 seconds, this can cut scoring time dramatically while preserving accuracy. FFmpeg's frame subsampling (`n_subsample`) further trades precision for speed during early iterations.

Typical searches converge in 3-5 VMAF evaluations.

### Search workflow

```
                         source.mp4
                             |
                   +---------+---------+
                   | duration >= 60s?  |
                   +---------+---------+
                    no |           | yes
                       |           |
                  use full     detect scenes
                  source as    extract samples
                  reference    concat to sample.mp4
                       |           |
                   +---+-----+-----+---+
                   |   reference clip   |
                   +--------+----------+
                            |
              +-------------+-------------+
              |  score CRF min and max    |
              |  (establish VMAF bounds)  |
              +-------------+-------------+
                            |
              +-------------+-------------+
              |  hybrid bisection loop    |
              |                           |
              |  1. bisect range (70%)    |
              |  2. interpolate (30%)     |
              |  3. blend + clamp         |
              |  4. encode at CRF         |
              |  5. VMAF score            |
              |  6. within tolerance?     |
              |     yes -> done           |
              |     no  -> narrow range   |
              +-------------+-------------+
                            |
                     selected CRF
```

### VMAF quality targets

| Score | Meaning | When to use |
|-------|---------|-------------|
| 95 | Near-transparent. Most viewers cannot distinguish from the source | Top rung of an ABR ladder, archival |
| 93 | High quality. Artifacts visible only under close inspection | Default target, good for most content |
| 90 | Good quality. Minor artifacts on complex scenes | Bandwidth-constrained delivery |

VMAF models are trained for specific viewing conditions. Netflix provides models for 4K, 1080p, and phone-sized displays. The model choice affects scores, so match the model to your target device.

## Prerequisites

The following tools must be installed and available on your `PATH`:

- [Go](https://go.dev/dl/) 1.24+
- [FFmpeg](https://ffmpeg.org/download.html) built with `--enable-libvmaf`
- [MediaInfo](https://mediaarea.net/en/MediaInfo/Download) (CLI version)
- [vmaf](https://github.com/Netflix/vmaf) CLI (only required for the `cambi` action)

### Building FFmpeg with libvmaf

Most package managers ship FFmpeg without libvmaf. A build script is included for macOS:

```bash
./scripts/build-ffmpeg-macos.sh
export PATH=$(pwd)/bin/ffmpeg:$PATH
```

This builds FFmpeg from source with libx264, libx265, SVT-AV1, libvpx, libvmaf, and hardware acceleration. Requires Homebrew and Xcode Command Line Tools. Takes about 5-10 minutes.

Verify your setup:

```bash
ffmpeg -filters 2>&1 | grep libvmaf
# should show: ... libvmaf  VV->V  Calculate the VMAF ...

mediainfo --Version
```

## Installation

```bash
git clone https://github.com/terranvigil/dynamic-crf.git
cd dynamic-crf
make build

./dynamic-crf -a search -i source.mp4
```

## Usage

```
dynamic-crf -a <action> -i <input> [options]
```

### Actions

| Action | Description | Requires `-o` |
|--------|-------------|:---:|
| `optimize` | Search for optimal CRF, encode the full video, and VMAF-score the result | Yes |
| `search` | Find the optimal CRF for a target VMAF without producing an output file | No |
| `encode` | Encode with a specific CRF or bitrate, then report its VMAF score | Yes |
| `inspect` | Write source metadata as JSON to `{source}_inspect.json` | No |
| `vmaf` | Calculate VMAF between a source (reference) and an encode (distorted) | Yes |
| `cambi` | Calculate [CAMBI](https://netflixtechblog.com/cambi-a-banding-artifact-detector-96777ae12fe2) banding artifact scores | Yes |

### Flags

**Search parameters:**

| Flag | Default | Description |
|------|---------|-------------|
| `-targetvmaf` | `95.0` | Target VMAF score (0-100) |
| `-tolerance` | `0.5` | How close to the target is close enough (VMAF points) |
| `-initialcrf` | `20` | Starting CRF for the search |
| `-mincrf` | `30` | Low-quality bound (higher CRF = lower quality = smaller file) |
| `-maxcrf` | `15` | High-quality bound (lower CRF = higher quality = larger file) |

> **Note on CRF naming:** In FFmpeg, lower CRF means higher quality. The flags here describe the search *bounds*: `-mincrf 30` is the minimum quality you'll accept (CRF 30), and `-maxcrf 15` is the maximum quality you'll search up to (CRF 15). The search finds the sweet spot between them.

**Encoding parameters:**

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `-codec` | | `libx264` | Video codec |
| `-height` | `-h` | | Output height in pixels (aspect ratio preserved) |
| `-width` | `-w` | | Output width in pixels (aspect ratio preserved) |
| `-maxbitrate` | `-mb` | | Peak bitrate cap, kbps |
| `-buffersize` | `-bs` | | HRD buffer size, kbps |
| `-tune` | `-t` | | Encoder tune: `animation`, `film`, `grain`, `psnr`, `ssim` |
| `-crf` | | | CRF value (encode action only) |
| `-bitrate` | | | Target bitrate, kbps (encode action only) |
| `-minbitrate` | | | Minimum bitrate, kbps (forces CBR, not recommended) |

**I/O:**

| Flag | Alias | Description |
|------|-------|-------------|
| `-action` | `-a` | Action to perform (required) |
| `-input` | `-i` | Path to source video (required) |
| `-output` | `-o` | Path to output file (`.mp4`, required for most actions) |

### Examples

```bash
# find the optimal CRF for a 1080p encode with bitrate cap
dynamic-crf -a optimize -i source.mp4 -o optimized.mp4 -h 1080 -mb 12000 -bs 48000
# => Found crf: 18, vmaf: 95.12, avg bitrate: 8400Kbps

# search only: find the CRF without encoding the full video
dynamic-crf -a search -i source.mp4 -h 720 -mb 6000 -bs 24000 -t animation
# => Found crf: 16, vmaf: 95.34

# encode at a known CRF and check the quality
dynamic-crf -a encode -i source.mp4 -o output.mp4 -crf 18 -h 720
# => Encode with crf: 18, vmaf: 94.21

# score an existing encode against the source
dynamic-crf -a vmaf -i source.mp4 -o encoded.mp4
# => VMAF: 93.45, avg bitrate: 6200Kbps

# check for banding artifacts (common in gradients and sky shots)
dynamic-crf -a cambi -i source.mp4 -o encoded.mp4
# => CAMBI max: 12.34, mean: 0.45

# dump source metadata for inspection
dynamic-crf -a inspect -i source.mp4
# => Wrote metadata to: source.mp4_inspect.json
```

## Project structure

```
cmd/                        CLI entry point and flag parsing
actions/                    High-level workflows (search, optimize, encode+score)
commands/                   Wrappers for ffmpeg, ffprobe, mediainfo, vmaf CLIs
inspect/                    Scene detection, sampling, and temporal analysis
model/                      Data structures for FFprobe and MediaInfo JSON output
testutil/                   Test helpers and fixture loading
fixtures/                   Test data and expected responses
```

## Development

```bash
make build          # compile binary
make test           # run unit tests (race detector enabled)
make fmt            # format with gofumpt
make lint           # run golangci-lint
make clean          # remove build artifacts
make vendor         # tidy and vendor dependencies
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Roadmap

- [ ] **Per-scene variable CRF**: use different CRF values for different scene complexities within a single encode, instead of one CRF for the whole file
- [ ] **Parallel encode/score**: run independent VMAF evaluations concurrently to reduce wall-clock search time
- [ ] **Scene transition detection**: identify fades, dissolves, and cross-fades to avoid sampling across transition boundaries
- [ ] **Anime VMAF model**: train a VMAF model tuned for animation content, which has different perceptual characteristics than live action
- [ ] **Resolution-specific models**: train and validate models for HD/TV and mobile viewing conditions

## References

### Key papers

- [Dynamic Optimizer: A Perceptual Video Encoding Optimization Framework](https://netflixtechblog.com/dynamic-optimizer-a-perceptual-video-encoding-optimization-framework-e19f1e3a277f), the Netflix system that inspired this project
- [Formulate the Optimal Encoding Ladder with VMAF](https://streaminglearningcenter.com/encoding/optimal_encoding_ladder_vmaf.html), Jan Ozer's practical methodology
- [Toward a Practical Perceptual Video Quality Metric](https://netflixtechblog.com/toward-a-practical-perceptual-video-quality-metric-653f208b9652), VMAF design and validation

### Further reading

- [CAMBI: A Banding Artifact Detector](https://netflixtechblog.com/cambi-a-banding-artifact-detector-96777ae12fe2), Netflix
- [CRF Guide](https://slhck.info/video/2017/02/24/crf-guide.html), Werner Robitza's explanation of CRF encoding
- [Finding the Just Noticeable Difference with VMAF](https://streaminglearningcenter.com/codecs/finding-the-just-noticeable-difference-with-netflix-vmaf.html)
- [A Practical Guide for VMAF](https://jina-liu.medium.com/a-practical-guide-for-vmaf-481b4d420d9c)
- [Instant Per-Title Encoding](https://www.mux.com/blog/instant-per-title-encoding), Mux

### Tools

- [Netflix VMAF](https://github.com/Netflix/vmaf), source, models, and documentation
- [FFmpeg libvmaf filter](https://ffmpeg.org/ffmpeg-filters.html#libvmaf)
- [FFmpeg VMAF usage guide](https://github.com/Netflix/vmaf/blob/master/resource/doc/ffmpeg.md)

### Test media for model training

- [4K Media](https://4kmedia.org/)
- [Xiph.org Video Test Media](https://media.xiph.org/video/)

## License

[MIT](LICENSE)
