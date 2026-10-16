package main

import "testing"

func TestSkillInstallLinksOnlyNavigateToExactVersion(t *testing.T) {
	route, err := skillInstallRoute("beeftv://skills/install?id=test-skill&version=release_1")
	if err != nil || route != "/skills?install=test-skill&version=release_1" {
		t.Fatalf("%s %v", route, err)
	}
	for _, bad := range []string{"beeftv://skills/install?id=x&version=1&url=https://evil.invalid", "beeftv://skills/install?id=x&version=1&version=2", "beeftv://skills/install?id=../secret&version=1", "beeftv://skills/install?id=x", "https://beeftv.app/skills/install?id=x&version=1", "beeftv://user@skills/install?id=x&version=1"} {
		if _, err := skillInstallRoute(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	if err := validateDesktopArgs([]string{"beeftv://skills/install?id=x&version=1"}); err != nil {
		t.Fatal(err)
	}
}

func TestOfficialSkillSemanticReleaseLink(t *testing.T) {
	route, e := skillInstallRoute("beeftv://skills/install?id=official-storyboard-text&version=official-storyboard-text-1.0.0")
	if e != nil || route == "" {
		t.Fatal("official release link rejected", e)
	}
}
