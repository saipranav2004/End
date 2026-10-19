package awsconnect

import (
	"fmt"
	"sort"
	"strings"
)

// Region is one AWS commercial region a customer can allow us to read.
type Region struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Group string `json:"group"`
	// OptIn regions are disabled in an AWS account until its owner enables
	// them; a sync of a disabled region finds nothing.
	OptIn bool `json:"opt_in"`
}

// Regions is the catalogue the console offers and the API accepts. It is the
// one place a new AWS region is added. Only the standard partition: the
// central event bus cannot receive from China or GovCloud.
var Regions = []Region{
	{"us-east-1", "US East (N. Virginia)", "United States", false},
	{"us-east-2", "US East (Ohio)", "United States", false},
	{"us-west-1", "US West (N. California)", "United States", false},
	{"us-west-2", "US West (Oregon)", "United States", false},
	{"ca-central-1", "Canada (Central)", "Canada", false},
	{"ca-west-1", "Canada West (Calgary)", "Canada", true},
	{"mx-central-1", "Mexico (Central)", "Mexico", true},
	{"sa-east-1", "South America (São Paulo)", "South America", false},
	{"eu-central-1", "Europe (Frankfurt)", "Europe", false},
	{"eu-central-2", "Europe (Zurich)", "Europe", true},
	{"eu-west-1", "Europe (Ireland)", "Europe", false},
	{"eu-west-2", "Europe (London)", "Europe", false},
	{"eu-west-3", "Europe (Paris)", "Europe", false},
	{"eu-south-1", "Europe (Milan)", "Europe", true},
	{"eu-south-2", "Europe (Spain)", "Europe", true},
	{"eu-north-1", "Europe (Stockholm)", "Europe", false},
	{"ap-south-1", "Asia Pacific (Mumbai)", "Asia Pacific", false},
	{"ap-south-2", "Asia Pacific (Hyderabad)", "Asia Pacific", true},
	{"ap-east-1", "Asia Pacific (Hong Kong)", "Asia Pacific", true},
	{"ap-east-2", "Asia Pacific (Taipei)", "Asia Pacific", true},
	{"ap-northeast-1", "Asia Pacific (Tokyo)", "Asia Pacific", false},
	{"ap-northeast-2", "Asia Pacific (Seoul)", "Asia Pacific", false},
	{"ap-northeast-3", "Asia Pacific (Osaka)", "Asia Pacific", false},
	{"ap-southeast-1", "Asia Pacific (Singapore)", "Asia Pacific", false},
	{"ap-southeast-2", "Asia Pacific (Sydney)", "Asia Pacific", false},
	{"ap-southeast-3", "Asia Pacific (Jakarta)", "Asia Pacific", true},
	{"ap-southeast-4", "Asia Pacific (Melbourne)", "Asia Pacific", true},
	{"ap-southeast-5", "Asia Pacific (Malaysia)", "Asia Pacific", true},
	{"ap-southeast-6", "Asia Pacific (New Zealand)", "Asia Pacific", true},
	{"ap-southeast-7", "Asia Pacific (Thailand)", "Asia Pacific", true},
	{"me-south-1", "Middle East (Bahrain)", "Middle East", true},
	{"me-central-1", "Middle East (UAE)", "Middle East", true},
	{"il-central-1", "Israel (Tel Aviv)", "Middle East", true},
	{"af-south-1", "Africa (Cape Town)", "Africa", true},
}

var regionIndex = func() map[string]int {
	m := make(map[string]int, len(Regions))
	for i, r := range Regions {
		m[r.Code] = i
	}
	return m
}()

// ErrInvalidRegions carries a message written for the person choosing them.
type ErrInvalidRegions struct{ Message string }

func (e *ErrInvalidRegions) Error() string { return e.Message }

// NormalizeRegions validates a region selection against the catalogue and
// returns it de-duplicated, in catalogue order (so the stored value does not
// depend on the order things were ticked).
func NormalizeRegions(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, &ErrInvalidRegions{Message: "Choose at least one region to read from."}
	}
	if len(in) > len(Regions)*2 {
		return nil, &ErrInvalidRegions{Message: "Too many regions in the request."}
	}
	seen := make(map[string]struct{}, len(in))
	unknown := make([]string, 0)
	for _, raw := range in {
		code := strings.ToLower(strings.TrimSpace(raw))
		if _, ok := regionIndex[code]; !ok {
			unknown = append(unknown, strings.TrimSpace(raw))
			continue
		}
		seen[code] = struct{}{}
	}
	if len(unknown) > 0 {
		if len(unknown) > 3 {
			unknown = append(unknown[:3], "…")
		}
		return nil, &ErrInvalidRegions{Message: fmt.Sprintf("Unknown AWS region: %s.", strings.Join(unknown, ", "))}
	}
	out := make([]string, 0, len(seen))
	for code := range seen {
		out = append(out, code)
	}
	sort.Slice(out, func(i, j int) bool { return regionIndex[out[i]] < regionIndex[out[j]] })
	return out, nil
}
