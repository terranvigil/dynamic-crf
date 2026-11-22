# dynamic-crf

Find the CRF that hits your target VMAF, without brute-force trial encodes.

A CLI tool for finding the optimal CRF (Constant Rate Factor) value for video encoding by targeting a specific [VMAF v1](https://github.com/Netflix/vmaf/blob/master/resource/doc/models_v1.md) quality score. `dynamic-crf` interpolates toward the target on a curve that's close to linear. Long sources are scored on a sample of their scenes. Most searches take three or four encodes.

## The problem

When encoding video with CRF, you're choosing a quality level, but the relationship between CRF and perceived quality depends entirely on the content. A talking-head at CRF 23 might score a VMAF of 97. The same CRF on an action sequence might drop to 85. The only way to know is to encode, score, and try again.

The naive approach (encode at many CRF values, VMAF-score each one, pick the best) works but is expensive. Each VMAF pass alone can take longer than the encode. For a per-title encoding pipeline producing multiple renditions, the cost multiplies fast.

## The approach

This project was inspired by Netflix's [per-title encoding optimization](https://netflixtechblog.com/dynamic-optimizer-a-perceptual-video-encoding-optimization-framework-e19f1e3a277f) and Jan Ozer's paper [Formulate the Optimal Encoding Ladder with VMAF](https://streaminglearningcenter.com/encoding/optimal_encoding_ladder_vmaf.html). The key insight: VMAF scoring is the bottleneck, not encoding. Netflix's own [work on frame subsampling](https://netflixtechblog.com/vmaf-the-journey-continues-44b51ee9ed12) showed that trading off some scoring accuracy, particularly early in the search, can dramatically reduce total computation time.

`dynamic-crf` reduces the cost in two ways.

**Fewer encodes.** VMAF against CRF is an S-curve. It's nearly flat close to 100 and steep in the middle, so interpolating on raw scores overshoots and plain bisection wastes probes. The search interpolates on logit(VMAF) instead, where the curve is much closer to a straight line. Once a CRF that meets the target and one that misses it are both known, each step is a secant between them. If one side keeps moving, the search falls back to bisection. Before the target is bracketed, it steps out from the first CRF using the measured slope, or a prior one until it has two points. No CRF is ever encoded twice.

**Cheaper scoring.** Sources of a minute or longer are scored on a sample of up to 15 scenes, 2 to 10 seconds each, taken at the sharpest scene cuts. The sample is lossless FFV1 in your temp directory. That can reach a few GB for 1080p, and tens of GB for 4K 10-bit, while the search runs. While searching, VMAF scores every 5th frame. The CRF the search settles on is then re-scored on every frame. If that exact score misses the target, the search steps toward quality until it doesn't. If the next CRF up only just missed on the coarse pass, it gets an exact score as well.

By default the search returns the highest CRF whose VMAF is at or above the target, which gives the smallest file. It stops early at the first CRF that scores inside `[target, target + tolerance)`, so "highest" holds to within the tolerance. `-policy closest` returns the CRF nearest the target on either side. In simulation over random curves the default averages about 3.2 coarse probes on a 15 to 30 range. None of those searches came back below target. In 99% of them the search found the highest CRF that meets it.

### Search workflow

```
                         source
                            |
                  +---------+---------+
                  | duration >= 60s?  |
                  +---------+---------+
                   no |           | yes
                      |           |
                 use full     detect scenes, keep the
                 source       15 sharpest cuts, extract
                      |       them losslessly (FFV1)
                      |           |
                  +---+-----+-----+---+
                  |   reference clip   |
                  +--------+-----------+
                           |
             +-------------+--------------+
             |  encode + score initial    |
             |  CRF (every 5th frame)     |
             +-------------+--------------+
                           |
             +-------------+--------------+
             |  within tolerance? -> done |
             |  bracketed?                |
             |    secant in logit space   |
             |    (bisect if stalling)    |
             |  one side only?            |
             |    step out by slope       |
             |  hit a bound or a VBV      |
             |    plateau? -> stop        |
             +-------------+--------------+
                           |
             +-------------+--------------+
             |  re-score the pick on      |
             |  every frame; step toward  |
             |  quality if it misses      |
             +-------------+--------------+
                           |
                     selected CRF
```

The search reports how it ended:

| Status | Meaning |
|--------|---------|
| `converged` | A score fell inside the tolerance window |
| `bracketed` | No CRF on the grid did. You get the better of the two that straddle the target |
| `target_exceeded_at_bound` | Even `-crf-high` beats the target |
| `target_unreachable` | Even `-crf-low` misses it |
| `plateau` | Lowering CRF stopped buying bitrate or quality. A `-maxbitrate` cap does this |
| `below_target` | The exact score still missed after stepping 3 CRF toward quality. The coarse and exact scores disagree badly, so try a lower `-subsample` |

Under the default policy, a result below the target always carries one of the last three statuses.

### VMAF v1 models

Scores come from Netflix's VMAF v1 models (`vmaf_v1.0.16`). With the default `-model auto`, the model depends on the source:

| Source | Model |
|--------|-------|
| 1080p and below | `vmaf_v1.0.16_3d0h` (1080p TV at 3 screen heights) |
| above 1080p | `vmaf_v1.0.16_1d5h_2160` (4K at 1.5 screen heights) |
| about 50 fps and up | the `_hfr` version of either |

Pass any other model name, such as `vmaf_v1.0.16_5d0h` for phones, or a path to a model JSON file. v1 needs a source of at least 240x160, or a short side of 270 for `5d0h` and 320 for `3d0h_2160`; for anything smaller, use `-model vmaf_v0.6.1`. When the picture is too small for the model's chroma feature, or its chroma is flat throughout (grayscale, smooth gradients), chroma drops out of the score and a warning says so. That happens below a short side of about 320 for `3d0h`, 540 for `5d0h` and 640 for `3d0h_2160`. `vmaf_v1.0.16_3d0h_2160` scores up to 110, so targets above 100 are valid with it.

Scoring follows what Netflix recommends for v1. Both sides are converted to 10-bit 4:2:0, and CAMBI, v1's banding feature, is told the encode's real size and bit depth. A downscaled rung is scaled back up (bicubic) to the source's size for scoring. During a search, a reference and encode that differ in length is an error. The `vmaf` action only warns about it, because you might be comparing files of different lengths on purpose. If libvmaf rejects the encode's size as CAMBI's hint (a small rung, a very small source, or wider than 7680), it's scored without that hint and a warning says so. v1 can't score a picture whose width plus height passes 12,092 (7680x4320 fits), so DCI 8K needs scaling down first.

### VMAF quality targets

| Score | Meaning | When to use |
|-------|---------|-------------|
| 95 | Near-transparent. Most viewers cannot distinguish from the source | Top rung of an ABR ladder, archival. The default target |
| 93 | High quality. Artifacts visible only under close inspection | Good for most content |
| 90 | Good quality. Minor artifacts on complex scenes | Bandwidth-constrained delivery |

These bands came from VMAF v0.6.1. Netflix calibrated v1 to line up with v0. Because v1 also counts banding and chroma, and doesn't reward sharpening, a given encode can still score differently. Check a few of your own titles before you lock in a target.

## Prerequisites

The following tools must be installed and available on your `PATH`:

- [Go](https://go.dev/dl/) 1.24+
- [FFmpeg](https://ffmpeg.org/download.html) and ffprobe, with libx264 or whichever encoder you use
- The `vmaf` CLI from [libvmaf](https://github.com/Netflix/vmaf) 3.2.1 or newer
- [MediaInfo](https://mediaarea.net/en/MediaInfo/Download) CLI (only for the `inspect` action)

### Building the vmaf CLI

libvmaf 3.2.0 ships the v1 models but can't load them, and that's the version some package managers still carry. Build 3.2.1 with the included script:

```bash
brew install meson ninja nasm
./scripts/build-vmaf.sh
export VMAF_BIN=~/.local/opt/vmaf-v1/bin/vmaf
```

The script installs the binary under `~/.local/opt/vmaf-v1` and the v1 model files under `~/.local/share/vmaf/model`. The models are also built into the binary, so `VMAF_MODEL_DIR` is optional. dynamic-crf reads `VMAF_BIN` and `VMAF_MODEL_DIR`, or the `-vmafbin` and `-vmafmodeldir` flags.

If the binary can't load v1, every score fails with an error naming the libvmaf version it needs.

### Building FFmpeg

FFmpeg doesn't need libvmaf, since scoring goes through the vmaf CLI. A build script for macOS is included if you want SVT-AV1, libvpx and VideoToolbox in one binary:

```bash
./scripts/build-ffmpeg-macos.sh
export PATH=$(pwd)/bin/ffmpeg:$PATH
```

This builds FFmpeg from source with libx264, libx265, SVT-AV1, dav1d, libvpx, opus and hardware acceleration. It needs Homebrew and the Xcode Command Line Tools, and takes about 5-10 minutes.

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
| `-targetvmaf` | `95.0` | Target VMAF score |
| `-tolerance` | `0.5` | Acceptance window in VMAF points above the target (either side with `-policy closest`). The search stops at the first CRF inside it |
| `-crf-low` | `15` | Lowest CRF searched: highest quality, largest file |
| `-crf-high` | `30` | Highest CRF searched: lowest quality, smallest file |
| `-initialcrf` | `20` | First CRF tried |
| `-crfstep` | `1` | CRF grid step. The range must be a whole number of steps. libx264 and libx265 take fractional CRF, so `0.5` or `0.25` work there |
| `-policy` | `floor` | `floor` returns the highest CRF at or above the target. `closest` returns the CRF nearest it |
| `-subsample` | `5` | Score every Nth frame while searching, but never fewer than 20 frames. The result is always re-scored on every frame |

`-maxcrf` and `-mincrf` still work as deprecated names for `-crf-low` and `-crf-high`. The old names described quality, so `-maxcrf 15` meant CRF 15.

**VMAF:**

| Flag | Default | Description |
|------|---------|-------------|
| `-model` | `auto` | `auto` picks a v1 model by height and frame rate. Also takes a model name or a `.json` path |
| `-vmafbin` | `$VMAF_BIN`, then `vmaf` | vmaf executable from libvmaf 3.2.1+ |
| `-vmafmodeldir` | `$VMAF_MODEL_DIR` | Directory of model JSON files, searched one level deep. Empty uses the models built into libvmaf |

**Encoding parameters:**

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `-codec` | | `libx264` | Video encoder. `libvpx-vp9` and `libaom-av1` get `-b:v 0` so CRF means constant quality |
| `-preset` | | | Encoder preset. A CRF found at one preset doesn't hold at another |
| `-tune` | `-t` | | Encoder tune, passed through to ffmpeg |
| `-pixfmt` | | | Output pixel format, such as `yuv420p`. Empty leaves it to the encoder |
| `-height` | | | Output height in pixels (aspect ratio kept if width is unset) |
| `-width` | | | Output width in pixels (aspect ratio kept if height is unset) |
| `-maxbitrate` | `-mb` | | Peak bitrate cap, kbps. Needs `-buffersize` |
| `-buffersize` | `-bs` | | VBV buffer size, kbits |
| `-acodec` | | `copy` | Audio in the `optimize` and `encode` output: an encoder, `copy`, or `none`. `optimize` checks that it fits in mp4 before searching. Search encodes never carry audio |
| `-abitrate` | | | Audio bitrate, kbps, when `-acodec` is an encoder |
| `-crf` | | | CRF value (encode action only) |
| `-bitrate` | | | Target bitrate, kbps (encode action only) |
| `-minbitrate` | | | Minimum bitrate, kbps (forces CBR, not recommended) |

**I/O:**

| Flag | Alias | Description |
|------|-------|-------------|
| `-action` | `-a` | Action to perform (required) |
| `-input` | `-i` | Path to source video (required by every action but `cambi`) |
| `-output` | `-o` | Output `.mp4` for `optimize` and `encode`, or the distorted file for `vmaf` and `cambi` |
| `-json` | | Print the result as JSON on stdout |
| `-verbose` | | Debug logging, including every ffmpeg and vmaf command |

Logs go to stderr. With `-json`, stdout holds only the result and can be piped. The `optimize` result includes `meetsTarget`, the full encode's own verdict, since a scene sample can flatter a title. `optimize` exits 0 either way, so check that field.

### Examples

```bash
# find the CRF for a 1080p rung with a bitrate cap, then encode the full video
dynamic-crf -a optimize -i source.mp4 -o optimized.mp4 -height 1080 -mb 12000 -bs 24000

# search only, at half-CRF steps, and read the result as JSON
dynamic-crf -a search -i source.mp4 -height 720 -crfstep 0.5 -json
# {
#   "crf": 21.5,
#   "vmaf": 95.21,
#   "status": "converged",
#   "exact": true,
#   ...
# }

# encode at a known CRF and check the quality
dynamic-crf -a encode -i source.mp4 -o output.mp4 -crf 18 -height 720

# score an existing encode against the source on the phone model
dynamic-crf -a vmaf -i source.mp4 -o encoded.mkv -model vmaf_v1.0.16_5d0h

# check for banding (common in gradients and sky shots); CAMBI only reads the encode
dynamic-crf -a cambi -o encoded.mp4

# dump source metadata to source.mp4_inspect.json
dynamic-crf -a inspect -i source.mp4
```

Without `-json`, the result is the last log line on stderr:

```
level=INFO msg="search complete" crf=21.5 vmaf=95.21 status=converged
```

## Project structure

```
cmd/                        CLI entry point, flag parsing and validation
actions/                    Search algorithm and workflows (search, optimize, encode+score)
commands/                   Wrappers for ffmpeg, ffprobe, mediainfo, vmaf CLIs
inspect/                    Scene detection, sampling, and temporal analysis
model/                      Data structures for FFprobe and MediaInfo JSON output
testutil/                   Test helpers and fixture loading
fixtures/                   Test data and expected responses
```

## Development

```bash
make build             # compile binary
make test              # run unit tests (race detector enabled)
make test-integration  # run integration tests: needs ffmpeg and a v1-capable vmaf
make test-fixtures     # fetch the Sintel and Big Buck Bunny fixtures
make vmaf              # build the vmaf CLI (scripts/build-vmaf.sh)
make fmt               # format with gofumpt
make lint              # run golangci-lint, test files included
make clean             # remove build artifacts
make vendor            # tidy and vendor dependencies
```

Unit tests need no external tools. The search is tested against simulated VMAF curves, including one measured from Big Buck Bunny. Integration tests skip themselves when `VMAF_BIN` can't score v1. Most generate their own clips. The rest skip when the fixtures are missing.

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

### Limitations

- The CRF defaults (15 to 30) suit libx264 and libx265. Other encoders have other scales, so set `-crf-low` and `-crf-high` for them.
- HDR isn't supported. VMAF v1's HDR models need a libvmaf build newer than 3.2.1.
- The `cambi` action handles pictures from 216 on the long side up to a width plus height of 6,092, just past UHD 4K (3840x2160). libvmaf 3.2.1's standalone CAMBI fails outside that, and also crashes on very wide, thin strips such as 4096x240.
- libvmaf 3.2.1 crashes on extreme aspect ratios, about 12:1 and wider depending on the model (such as 3840x320). Real video doesn't hit this.
- Encodes keep every source frame with its own timestamp, so variable-rate sources stay variable-rate. Interlaced sources are encoded and scored as progressive frames.

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
- [VMAF v1 models](https://github.com/Netflix/vmaf/blob/master/resource/doc/models_v1.md), model names, viewing conditions and recommended settings

### Test media for model training

- [4K Media](https://4kmedia.org/)
- [Xiph.org Video Test Media](https://media.xiph.org/video/)

## License

[MIT](LICENSE)
