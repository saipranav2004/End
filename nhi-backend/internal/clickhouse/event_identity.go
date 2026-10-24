package clickhouse

import "strings"

// How log_events names the identity an event belongs to.
//
// For most workload families log_events.identity_name is the identity's own
// key. An ECS task's identity_name is the task label ("standalone:<family>_<role>_task"),
// while the identity this console tracks is the task role. For ECS the role ARN
// is therefore read from request_parameters.roleArn (the AssumeRole call), and
// from the role_arn column on events that carry no such parameter.
//
// toString() keeps the expression valid whether request_parameters is a String
// or a JSON column.

const ecsRoleArn = `if(JSONExtractString(toString(request_parameters), 'roleArn') != '',
	JSONExtractString(toString(request_parameters), 'roleArn'),
	if(role_arn != '', role_arn, identity_name))`

// EventIdentity is the identity key of a log_events row, for queries over many
// identities at once (it needs {names:Array(String)}, the requested keys): an
// ECS row counts under its role ARN when that was asked for, under its own
// identity_name otherwise; every other row under identity_name.
const EventIdentity = `if(source_type = 'ECS' AND has({names:Array(String)}, ` + ecsRoleArn + `), ` + ecsRoleArn + `, identity_name)`

// IdentityEquals is the predicate "this row belongs to the identity bound as
// {identity_name:String}" for one known source type. Non-ECS keeps the plain
// column comparison; ECS matches the role ARN, or the row's own identity_name
// for a key that is not a role ARN.
func IdentityEquals(sourceType string) string {
	if strings.EqualFold(strings.TrimSpace(sourceType), "ECS") {
		return `(` + ecsRoleArn + ` = {identity_name:String} OR identity_name = {identity_name:String})`
	}
	return `identity_name = {identity_name:String}`
}
