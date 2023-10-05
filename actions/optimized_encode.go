package actions

import "github.com/terranvigil/dynamic-crf/commands"

type OptimizedEncoded struct {
	TranscodeConfig *commands.TranscodeConfig
	SourcePath      string
	TargetPath      string
}

func NewOptimizedEncoded(cfg *commands.TranscodeConfig, source string, target string) *OptimizedEncoded {
	return &OptimizedEncoded{
		TranscodeConfig: cfg,
		SourcePath:      source,
		TargetPath:      target,
	}
}

func (e *OptimizedEncoded) Run() error {
	// TODO
	return nil
}
