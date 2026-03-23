package proxy

import (
	"strings"
	"testing"
)

func TestSanitizeBranch_Slashes(t *testing.T) {
	got, err := SanitizeBranch("feat/auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "feat-auth" {
		t.Errorf("SanitizeBranch(%q) = %q, want %q", "feat/auth", got, "feat-auth")
	}
}

func TestSanitizeBranch_Dots(t *testing.T) {
	got, err := SanitizeBranch("release.1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "release-1-0" {
		t.Errorf("SanitizeBranch(%q) = %q, want %q", "release.1.0", got, "release-1-0")
	}
}

func TestSanitizeBranch_Underscores(t *testing.T) {
	got, err := SanitizeBranch("fix_login_bug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "fix-login-bug" {
		t.Errorf("SanitizeBranch(%q) = %q, want %q", "fix_login_bug", got, "fix-login-bug")
	}
}

func TestSanitizeBranch_Uppercase(t *testing.T) {
	got, err := SanitizeBranch("Feature/Auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "feature-auth" {
		t.Errorf("SanitizeBranch(%q) = %q, want %q", "Feature/Auth", got, "feature-auth")
	}
}

func TestSanitizeBranch_ConsecutiveSpecialChars(t *testing.T) {
	got, err := SanitizeBranch("feat//auth--v2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "feat-auth-v2" {
		t.Errorf("SanitizeBranch(%q) = %q, want %q", "feat//auth--v2", got, "feat-auth-v2")
	}
}

func TestSanitizeBranch_LeadingTrailingSpecial(t *testing.T) {
	got, err := SanitizeBranch("/feat/auth/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "feat-auth" {
		t.Errorf("SanitizeBranch(%q) = %q, want %q", "/feat/auth/", got, "feat-auth")
	}
}

func TestSanitizeBranch_Unicode(t *testing.T) {
	got, err := SanitizeBranch("feat/日本語")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "feat" {
		t.Errorf("SanitizeBranch(%q) = %q, want %q", "feat/日本語", got, "feat")
	}
}

func TestSanitizeBranch_Empty(t *testing.T) {
	_, err := SanitizeBranch("")
	if err == nil {
		t.Error("expected error for empty branch name")
	}
}

func TestSanitizeBranch_AllSpecialChars(t *testing.T) {
	_, err := SanitizeBranch("///...")
	if err == nil {
		t.Error("expected error for branch name that sanitizes to empty")
	}
}

func TestSanitizeBranch_MaxLength(t *testing.T) {
	long := strings.Repeat("a", 100)
	got, err := SanitizeBranch(long)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) > maxDNSLabelLength {
		t.Errorf("label length %d exceeds max %d", len(got), maxDNSLabelLength)
	}
}

func TestSanitizeBranch_MaxLengthHashUniqueness(t *testing.T) {
	long1 := strings.Repeat("a", 100) + "1"
	long2 := strings.Repeat("a", 100) + "2"

	got1, err := SanitizeBranch(long1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got2, err := SanitizeBranch(long2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got1 == got2 {
		t.Errorf("truncated labels should differ due to hash suffix: %q == %q", got1, got2)
	}
}

func TestSanitizeBranch_ExactlyMaxLength(t *testing.T) {
	exact := strings.Repeat("a", maxDNSLabelLength)
	got, err := SanitizeBranch(exact)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != exact {
		t.Errorf("branch exactly at max length should not be truncated: got %q", got)
	}
}

func TestSanitizeBranch_SimpleValid(t *testing.T) {
	got, err := SanitizeBranch("main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "main" {
		t.Errorf("SanitizeBranch(%q) = %q, want %q", "main", got, "main")
	}
}

func TestSanitizeBranch_Hyphen(t *testing.T) {
	got, err := SanitizeBranch("feat-auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "feat-auth" {
		t.Errorf("SanitizeBranch(%q) = %q, want %q", "feat-auth", got, "feat-auth")
	}
}

func TestBuildHostname_DefaultBranch(t *testing.T) {
	got, err := BuildHostname("api", "main", "myapp", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "api.myapp.localhost" {
		t.Errorf("BuildHostname = %q, want %q", got, "api.myapp.localhost")
	}
}

func TestBuildHostname_FeatureBranch(t *testing.T) {
	got, err := BuildHostname("api", "feat/auth", "myapp", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "api.feat-auth.myapp.localhost" {
		t.Errorf("BuildHostname = %q, want %q", got, "api.feat-auth.myapp.localhost")
	}
}

func TestBuildHostname_NoDefaultBranch(t *testing.T) {
	got, err := BuildHostname("web", "main", "myapp", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "main") {
		t.Errorf("expected hostname to contain branch when no default branch set: %q", got)
	}
}
