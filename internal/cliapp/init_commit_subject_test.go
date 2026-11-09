package cliapp_test

import (
	"testing"
)

func TestInitSetUpCommitKeepsItsSubject(t *testing.T) {
	repo := setUpInitRepo(t)

	if subject := headSubject(t, repo); subject != "sdd: init .sdd/ metadata directory" {
		t.Errorf("a set-up should commit under its own subject, got %q", subject)
	}
}

func TestInitRefreshCommitsUnderTheGenericSubject(t *testing.T) {
	repo := setUpInitRepo(t)

	runInit(t, repo, "--verify-mode", "prose")

	if subject := headSubject(t, repo); subject != "sdd: refresh sdd setup" {
		t.Errorf("a refresh should commit under the generic subject, got %q", subject)
	}
}
