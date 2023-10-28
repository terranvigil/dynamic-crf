# dynamic-crf

## Background

This project was inspired by Jan Ozer's paper "Formulate the Optimal Encoding Ladder with VMAF". Essentially, the first step in generating a per-title optimized ABR ladder is identifying the bitrate of the top rung of of the ladder. So if we're talking about 4k or 3840p, we know we want a high objective quality score. If we say a VMAF of 95 will generally give us a rendition that is indistinguishable, or nearly so, from the original by viewers we need to find the encoding settings to achieve this.

The simplest approach is brute force. Encode a 4k rendition using equally spaced target bitrates until you get a VMAF of 95. But this is costly. The multiple encodes are expensive and the VMAF scoring is even more so by quite a bit! The VMAF documentation offers some suggestions, such as frame skipping mentioned [here](https://netflixtechblog.com/vmaf-the-journey-continues-44b51ee9ed12). This was used by Netflix to get VMAF analysis down to real-time for 4k encodes.

If we can trade off some accuracy, particularly in early part of the search, we can speed up the VMAF calculation significantly. To do this, we will:

1. Select a target VMAF score we want, along with a tolerance percentage
2. Select a max and min CRF range we will search.
3. Rather than score the entire title, generate a sample containing a representative selection of parts of scenes as our reference.
4. Begin the search by finding the VMAF values of the min and max CRF values.
5. Continue using an interpolation search, described [here](https://www.geeksforgeeks.org/g-fact-84/), until we find a VMAF value within our target threshold or if we get the same CRF chosen more than once. The latter scenario occurs if we're as close as we are going to get to the target.
6. In early interations of the search, use a decreasing number of skipped frames as the speed vs trade-off is tolerable. 

### Models

Typically, there are three model types trained: 4K, HD and Phone

### Scoring

- 93 is generally a good target
- 95 should be nearly indistinguishable from the reference
- Anything over 90 should look good

### Building

`make build`

### Examples

* Perform an optimized encode using calculated CRF:<br />
`dynamic-crf -i source.mp4 -o optimized.mp4 -a optimize -h 1080 -mb 12000 -bs 48000 -maxcrf 15`

* Search for the best CRF given a source and encoding settings:<br />
`dynamic-crf -i source.mp4 -a optimize -h 720 -mb 6000 -bs 24000 -t animation -maxcrf 15`

* Run a test encode given a source and encoding settings:<br />
`dynamic-crf -i source.mp4 -a search -h 720 -mb 6000 -bs 24000`

### TODO

1. Need to train VMAF model for Anime, requires manual objective ranking
2. Need to train models for HD/TV resolutions and phone resolutions
3. Could be optimized per scene (CRF per scene)
4. Parallelize
5. Add ability to detect scene changes when there is a transition such as a fade

## Test Sources for Training Models

- https://4kmedia.org/
- https://media.xiph.org/video/

## Related Repositories

- https://github.com/Netflix/vmaf
- https://github.com/JNoDuq/videobench

## Related documents

- https://ffmpeg.org/ffmpeg-filters.html#libvmaf
- https://ottverse.com/top-rung-of-encoding-bitrate-ladder-abr-video-streaming
- https://arxiv.org/pdf/2103.07564.pdf
- https://www.streamingmedia.com/Articles/ReadArticle.aspx?ArticleID=139246
- https://ieeexplore.ieee.org/document/9477488
- https://streaminglearningcenter.com/encoding/optimal_encoding_ladder_vmaf.html
- https://streaminglearningcenter.com/codecs/finding-the-just-noticeable-difference-with-netflix-vmaf.html- https://jina-liu.medium.com/a-practical-guide-for-vmaf-481b4d420d9c
- https://realnetworks.com/sites/default/files/vmaf_reproducibility_ieee.pdf
- https://slhck.info/video/2017/02/24/crf-guide.html
- https://netflixtechblog.com/dynamic-optimizer-a-perceptual-video-encoding-optimization-framework-e19f1e3a277f
- https://eyevinntechnology.medium.com/keep-an-eye-on-the-video-quality-b9bcb58dd5a1
- https://netflixtechblog.com/toward-a-practical-perceptual-video-quality-metric-653f208b9652
- https://netflixtechblog.com/vmaf-the-journey-continues-44b51ee9ed12
- https://blog.twitter.com/engineering/en_us/topics/infrastructure/2020/introducing-vmaf-percentiles-for-video-quality-measurements
- https://kokomins.wordpress.com/2019/10/10/anime-encoding-guide-for-x265-and-why-to-never-use-flac/
- https://github.com/Netflix/vmaf/blob/master/resource/doc/ffmpeg.md
- https://www.streamingmedia.com/Articles/ReadArticle.aspx?ArticleID=139246
- https://www.mux.com/blog/instant-per-title-encoding
- https://netflixtechblog.com/cambi-a-banding-artifact-detector-96777ae12fe2
