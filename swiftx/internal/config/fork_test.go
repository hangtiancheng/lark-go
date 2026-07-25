package config

import "testing"

// enable_fork defaults to enabled, and an explicit false in the config must
// actually turn it off. Storing it as a plain bool would make "not set" and
// "set to false" share the same zero value, so the latter could never disable it.
func TestForkEnabledDefaultsOn(t *testing.T) {
	cfg := &AppConfig{}
	if !cfg.ForkEnabled() {
		t.Fatal("fork should be enabled when enable_fork is not set in the config")
	}
}

func TestForkEnabledExplicitFalse(t *testing.T) {
	off := false
	cfg := &AppConfig{EnableFork: &off}
	if cfg.ForkEnabled() {
		t.Fatal("fork should be disabled when the config sets enable_fork: false")
	}

	on := true
	cfg.EnableFork = &on
	if !cfg.ForkEnabled() {
		t.Fatal("fork should be enabled when the config sets enable_fork: true")
	}
}

// A later-loaded config only overrides when it explicitly sets enable_fork;
// otherwise it inherits the value from the previous layer.
func TestMergeConfigFork(t *testing.T) {
	off := false

	base := &AppConfig{EnableFork: &off}
	merged := mergeConfig(base, &AppConfig{})
	if merged.ForkEnabled() {
		t.Fatal("a later layer without enable_fork must not override the previous layer's false")
	}

	base2 := &AppConfig{}
	merged2 := mergeConfig(base2, &AppConfig{EnableFork: &off})
	if merged2.ForkEnabled() {
		t.Fatal("a later layer setting enable_fork: false should override the default value")
	}
}
