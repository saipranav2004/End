package awsconnect

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRegionCatalogue(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Regions {
		if seen[r.Code] {
			t.Fatalf("duplicate region %s", r.Code)
		}
		seen[r.Code] = true
		if !regionRE.MatchString(r.Code) || r.Name == "" || r.Group == "" {
			t.Fatalf("malformed catalogue entry %+v", r)
		}
	}
	for _, must := range []string{"us-east-1", "ap-south-1", "eu-west-1"} {
		if !seen[must] {
			t.Fatalf("catalogue lacks %s", must)
		}
	}
}

func TestNormalizeRegions(t *testing.T) {
	got, err := NormalizeRegions([]string{" AP-SOUTH-1 ", "us-east-1", "ap-south-1"})
	if err != nil {
		t.Fatal(err)
	}
	// Catalogue order, de-duplicated, lower-cased.
	if want := []string{"us-east-1", "ap-south-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for in, wantText := range map[string]string{"": "at least one", "mars-north-1": "Unknown AWS region: mars-north-1", "cn-north-1": "Unknown", "us-gov-west-1": "Unknown"} {
		var regions []string
		if in != "" {
			regions = []string{in}
		}
		_, err := NormalizeRegions(regions)
		var target *ErrInvalidRegions
		if !errors.As(err, &target) || !strings.Contains(target.Message, wantText) {
			t.Fatalf("NormalizeRegions(%q) = %v, want message containing %q", in, err, wantText)
		}
	}
}

func TestPlatformForTenant(t *testing.T) {
	p, err := PlatformForTenant("189160505560", "arn:aws:events:ap-south-1:189160505560:event-bus/nhi-central-bus", "NHIPlatformAssumer")
	if err != nil {
		t.Fatal(err)
	}
	if p.Region != "ap-south-1" || p.EventBusName != "nhi-central-bus" || p.AssumerRoleARN != "arn:aws:iam::189160505560:role/NHIPlatformAssumer" {
		t.Fatalf("platform = %+v", p)
	}
	bad := [][3]string{
		{"189160505560", "arn:aws:events:ap-south-1:999999999999:event-bus/b", "NHIPlatformAssumer"}, // bus in another account
		{"189160505560", "not-an-arn", "NHIPlatformAssumer"},
		{"189160505560", "arn:aws:events:ap-south-1:189160505560:event-bus/b", "bad name!"},
		{"1891605055", "arn:aws:events:ap-south-1:1891605055:event-bus/b", "NHIPlatformAssumer"},
	}
	for _, c := range bad {
		if _, err := PlatformForTenant(c[0], c[1], c[2]); !errors.Is(err, ErrPlatformMisconfigured) {
			t.Fatalf("PlatformForTenant(%v) = %v, want ErrPlatformMisconfigured", c, err)
		}
	}
}
