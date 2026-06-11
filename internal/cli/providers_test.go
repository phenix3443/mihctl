package cli

import (
	"reflect"
	"testing"

	"github.com/phenix3443/mihomo-companion/internal/configgen"
	"github.com/phenix3443/mihomo-companion/internal/mihomo"
)

func TestProvidersProbeCommandBuildsServiceScope(t *testing.T) {
	originalLoadEnv := loadEnv
	originalRunProvidersProbe := runProvidersProbe
	t.Cleanup(func() {
		loadEnv = originalLoadEnv
		runProvidersProbe = originalRunProvidersProbe
	})

	env := &mihomo.Env{}
	loadEnv = func() (*mihomo.Env, error) {
		return env, nil
	}

	var gotEnv *mihomo.Env
	var gotScope configgen.ProbeScope
	runProvidersProbe = func(actualEnv *mihomo.Env, scope configgen.ProbeScope) error {
		gotEnv = actualEnv
		gotScope = scope
		return nil
	}

	cmd := newProvidersCmd()
	cmd.SetArgs([]string{"probe", "--provider", "bywave", "--provider", "jisu", "--group", "stable"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if gotEnv != env {
		t.Fatal("probe command did not use loaded env")
	}
	wantScope := configgen.ProbeScope{
		Providers: []string{"bywave", "jisu"},
		Services:  []string{"stable"},
		Mode:      configgen.ProbeModeService,
	}
	if !reflect.DeepEqual(gotScope, wantScope) {
		t.Fatalf("scope = %#v, want %#v", gotScope, wantScope)
	}
}
