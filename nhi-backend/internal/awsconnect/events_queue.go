package awsconnect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// ErrEventQueueNotConfigured: no SQS queue is set (aws_integration.event_queue_url).
var ErrEventQueueNotConfigured = errors.New("event queue is not configured")

var sqsRegionRE = regexp.MustCompile(`^https://sqs\.([a-z0-9-]+)\.`)

const (
	queuePeekRounds = 3  // receive calls per check
	queuePeekBatch  = 10 // SQS maximum per call
	queuePeekWaitS  = 2  // long-poll seconds per call
)

// PeekEventsFrom looks at a few messages of the central event queue and
// reports whether one came from accountID. It only peeks: messages are never
// deleted and are made visible again at once (VisibilityTimeout 0), so the
// pipeline that consumes the queue loses nothing. Each message body is the
// EventBridge event JSON, whose top-level "account" is set by AWS and cannot
// be forged by the sender.
func (c *Client) PeekEventsFrom(ctx context.Context, p Platform, accountID string) (bool, error) {
	if c.eventQueueURL == "" {
		return false, ErrEventQueueNotConfigured
	}
	if !accountIDRE.MatchString(accountID) {
		return false, fmt.Errorf("invalid account id")
	}
	cfg := c.platformConfig(p)
	if m := sqsRegionRE.FindStringSubmatch(c.eventQueueURL); m != nil {
		cfg.Region = m[1] // the queue's own region, which may differ from the bus's
	}
	client := sqs.NewFromConfig(cfg)
	for i := 0; i < queuePeekRounds; i++ {
		out, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(c.eventQueueURL),
			MaxNumberOfMessages: queuePeekBatch,
			VisibilityTimeout:   0,
			WaitTimeSeconds:     queuePeekWaitS,
		})
		if err != nil {
			return false, fmt.Errorf("peek event queue: %w", err)
		}
		if messagesFromAccount(out.Messages, accountID) {
			return true, nil
		}
	}
	return false, nil
}

func messagesFromAccount(msgs []sqstypes.Message, accountID string) bool {
	for _, m := range msgs {
		if m.Body != nil && bodyAccount(*m.Body) == accountID {
			return true
		}
	}
	return false
}

// bodyAccount reads "account" from an EventBridge event, also when it is
// wrapped in an SNS-style {"Message": "<event json>"} envelope.
func bodyAccount(body string) string {
	var ev struct {
		Account string `json:"account"`
		Message string `json:"Message"`
	}
	if json.Unmarshal([]byte(body), &ev) != nil {
		return ""
	}
	if ev.Account == "" && ev.Message != "" {
		var inner struct {
			Account string `json:"account"`
		}
		_ = json.Unmarshal([]byte(ev.Message), &inner)
		return inner.Account
	}
	return ev.Account
}
