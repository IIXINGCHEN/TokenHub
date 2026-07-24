package litellm

import (
	"context"
	"fmt"
	"os"

	"tokenhub/backend/internal/migration/bundle"
	"tokenhub/backend/internal/migration/source"
)

const (
	AdapterName           = "litellm"
	AdapterVersion        = "0.1.0"
	SupportedVersionRange = ">=1.52.0, <1.70.0"
)

type Adapter struct{}

func init() {
	source.Register(Adapter{})
}

func (Adapter) Name() string { return AdapterName }

func (Adapter) SupportedVersions() string { return SupportedVersionRange }

func (Adapter) Extract(ctx context.Context, opts source.ExtractOptions) (*bundle.CanonicalMigrationBundle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.InputPath == "" {
		return nil, fmt.Errorf("litellm: input path is required")
	}
	data, err := os.ReadFile(opts.InputPath)
	if err != nil {
		return nil, fmt.Errorf("litellm: read config: %w", err)
	}
	config, err := ParseConfig(data)
	if err != nil {
		return nil, err
	}
	return BuildBundle(config, BuildOptions{
		OriginURL: opts.OriginURL,
		Metadata:  opts.Metadata,
	})
}
