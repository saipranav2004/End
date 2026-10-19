// Package awsconnect makes the AWS calls behind "Connect AWS Account": assume
// a client's role, make one read with it, allow the client's account on the
// central event bus, and look for an event the client has forwarded.
package awsconnect

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Only the commercial partition: the central bus lives there, and EventBridge
// cannot deliver across partitions.
var (
	// Path segments are printable ASCII (IAM's own rule); the name is IAM's
	// role-name alphabet, at most 64 characters.
	roleARNRE        = regexp.MustCompile(`^arn:aws:iam::(\d{12}):role/((?:[\x21-\x7E]+/)?)([\w+=,.@-]{1,64})$`)
	eventBusARNRE    = regexp.MustCompile(`^arn:aws:events:([a-z]{2}(?:-[a-z]+)+-\d{1,2}):(\d{12}):event-bus/([\w.\-]{1,256})$`)
	accountIDRE      = regexp.MustCompile(`^\d{12}$`)
	regionRE         = regexp.MustCompile(`^[a-z]{2}(?:-[a-z]+)+-\d{1,2}$`)
	otherIAMEntityRE = regexp.MustCompile(`^arn:aws[\w-]*:iam::\d{12}:(user|group|policy|instance-profile|oidc-provider|saml-provider)/`)
)

const maxRoleARNLength = 1024

// RoleARN is a validated IAM role ARN.
type RoleARN struct {
	ARN       string
	AccountID string
	Name      string
}

// ErrInvalidRoleARN carries a message written for the person who pasted it.
type ErrInvalidRoleARN struct{ Message string }

func (e *ErrInvalidRoleARN) Error() string { return e.Message }

// ParseRoleARN validates a pasted role ARN and says, in plain words, what is
// wrong with it when it is not one - the common mistakes are pasting the role
// name, an instance profile ARN, or a user ARN.
func ParseRoleARN(raw string) (RoleARN, error) {
	value := strings.TrimSpace(raw)
	switch {
	case value == "":
		return RoleARN{}, &ErrInvalidRoleARN{Message: "Paste the ARN of the role you created."}
	case len(value) > maxRoleARNLength:
		return RoleARN{}, &ErrInvalidRoleARN{Message: "That is too long to be a role ARN."}
	}
	if m := otherIAMEntityRE.FindStringSubmatch(value); m != nil {
		return RoleARN{}, &ErrInvalidRoleARN{Message: fmt.Sprintf("That is an IAM %s ARN. Paste the role's ARN - it contains \":role/\".", strings.ReplaceAll(m[1], "-", " "))}
	}
	m := roleARNRE.FindStringSubmatch(value)
	if m == nil {
		if strings.HasPrefix(value, "arn:aws-") {
			return RoleARN{}, &ErrInvalidRoleARN{Message: "Only accounts in the standard AWS partition can be connected."}
		}
		return RoleARN{}, &ErrInvalidRoleARN{Message: "That is not a role ARN. It looks like arn:aws:iam::123456789012:role/RoleName."}
	}
	return RoleARN{ARN: value, AccountID: m[1], Name: m[3]}, nil
}

// Platform is the platform's own AWS setup, as stored in
// nhi_schema.aws_platform_settings.
type Platform struct {
	AccountID      string
	Region         string
	EventBusARN    string
	EventBusName   string
	AssumerRoleARN string
}

// ErrPlatformMisconfigured means the stored platform values contradict each
// other. It is an operator error, never shown to clients verbatim.
var ErrPlatformMisconfigured = errors.New("aws platform settings are inconsistent")

// NewPlatform validates the stored platform values against each other: the
// bus and the assumer role must both be in the platform account, and the bus
// in the platform region. A mismatch here would hand every client a setup that
// can never pass.
func NewPlatform(accountID, region, eventBusARN, assumerRoleARN string) (Platform, error) {
	accountID, region = strings.TrimSpace(accountID), strings.TrimSpace(region)
	eventBusARN, assumerRoleARN = strings.TrimSpace(eventBusARN), strings.TrimSpace(assumerRoleARN)
	if !accountIDRE.MatchString(accountID) || !regionRE.MatchString(region) {
		return Platform{}, fmt.Errorf("%w: account_id or region is malformed", ErrPlatformMisconfigured)
	}
	bus := eventBusARNRE.FindStringSubmatch(eventBusARN)
	if bus == nil {
		return Platform{}, fmt.Errorf("%w: event_bus_arn is malformed", ErrPlatformMisconfigured)
	}
	if bus[1] != region || bus[2] != accountID {
		return Platform{}, fmt.Errorf("%w: event_bus_arn is not in account %s, region %s", ErrPlatformMisconfigured, accountID, region)
	}
	assumer, err := ParseRoleARN(assumerRoleARN)
	if err != nil {
		return Platform{}, fmt.Errorf("%w: assumer_role_arn is malformed", ErrPlatformMisconfigured)
	}
	if assumer.AccountID != accountID {
		return Platform{}, fmt.Errorf("%w: assumer_role_arn is not in account %s", ErrPlatformMisconfigured, accountID)
	}
	return Platform{
		AccountID:      accountID,
		Region:         region,
		EventBusARN:    eventBusARN,
		EventBusName:   bus[3],
		AssumerRoleARN: assumer.ARN,
	}, nil
}

var roleNameRE = regexp.MustCompile(`^[\w+=,.@-]{1,64}$`)

// PlatformForTenant builds the platform values for one tenant from its row in
// nhi_schema.tenants: our AWS account and the central event bus it forwards
// to. The region is the bus's; the assumer role (the principal every customer
// trust policy names) is assumerRoleName in that same account.
func PlatformForTenant(accountID, eventBusARN, assumerRoleName string) (Platform, error) {
	bus := eventBusARNRE.FindStringSubmatch(strings.TrimSpace(eventBusARN))
	if bus == nil {
		return Platform{}, fmt.Errorf("%w: event_bus_arn is malformed", ErrPlatformMisconfigured)
	}
	name := strings.TrimSpace(assumerRoleName)
	if !roleNameRE.MatchString(name) {
		return Platform{}, fmt.Errorf("%w: assumer role name is malformed", ErrPlatformMisconfigured)
	}
	accountID = strings.TrimSpace(accountID)
	return NewPlatform(accountID, bus[1], eventBusARN, "arn:aws:iam::"+accountID+":role/"+name)
}

// BusStatementID is the Sid of the statement that lets one client account put
// events on the central bus. Deterministic, so adding it again is a no-op.
func BusStatementID(accountID string) string {
	return "nhi-client-" + accountID
}
