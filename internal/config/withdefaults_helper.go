package config

// WithDefaults returns a copy of cfg with defaults/path expansion applied.
func WithDefaults(cfg Config) Config {
	next := cfg
	next.applyDefaults()
	next.expandPaths()
	return next
}
