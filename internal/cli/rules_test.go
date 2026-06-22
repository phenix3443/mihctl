package cli

import (
	"testing"

	"github.com/phenix3443/mihomo-companion/internal/mihomo"
)

func TestRulesUpdateCommandSyncsRules(t *testing.T) {
	originalLoadEnv := loadEnv
	originalRunRulesSync := runRulesSync
	t.Cleanup(func() {
		loadEnv = originalLoadEnv
		runRulesSync = originalRunRulesSync
	})

	env := &mihomo.Env{}
	loadEnv = func() (*mihomo.Env, error) {
		return env, nil
	}

	var gotEnv *mihomo.Env
	runRulesSync = func(actualEnv rulesSyncEnv) error {
		gotEnv, _ = actualEnv.(*mihomo.Env)
		return nil
	}

	cmd := newRulesCmd()
	cmd.SetArgs([]string{"update"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if gotEnv != env {
		t.Fatal("update command did not sync rules with loaded env")
	}
}

func TestRulesSyncCommandSyncsRules(t *testing.T) {
	originalLoadEnv := loadEnv
	originalRunRulesSync := runRulesSync
	t.Cleanup(func() {
		loadEnv = originalLoadEnv
		runRulesSync = originalRunRulesSync
	})

	env := &mihomo.Env{}
	loadEnv = func() (*mihomo.Env, error) {
		return env, nil
	}

	var gotEnv *mihomo.Env
	runRulesSync = func(actualEnv rulesSyncEnv) error {
		gotEnv, _ = actualEnv.(*mihomo.Env)
		return nil
	}

	cmd := newRulesCmd()
	cmd.SetArgs([]string{"sync"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if gotEnv != env {
		t.Fatal("sync command did not sync rules with loaded env")
	}
}
