package awsbroker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// Provisioner ensures SQS queues, SNS topics, and SNS→SQS subscriptions exist.
// All operations are idempotent: running the same Ensure* call twice is safe.
//
// Works against both real AWS and LocalStack — only the endpoint resolution
// differs, which is configured on the underlying clients.
type Provisioner struct {
	sqs SQSAPI
	sns SNSAPI
}

func NewProvisioner(sqsClient SQSAPI, snsClient SNSAPI) *Provisioner {
	return &Provisioner{sqs: sqsClient, sns: snsClient}
}

// QueueSpec describes a queue to create.
type QueueSpec struct {
	Name string
	// EnableDLQ creates a companion <name>-dlq queue and wires a redrive policy
	// pointing to it. Matches the terragrunt module behavior.
	EnableDLQ bool
	// DLQMaxReceiveCount is the number of receive attempts before a message is
	// moved to the DLQ. Defaults to 5 when EnableDLQ is true.
	DLQMaxReceiveCount int
}

// EnsureQueue creates the SQS queue if missing and returns its ARN.
// When spec.EnableDLQ is true, a <name>-dlq queue is also created and a redrive
// policy is attached to the main queue.
func (p *Provisioner) EnsureQueue(ctx context.Context, spec QueueSpec) (queueArn string, err error) {
	if spec.Name == "" {
		return "", fmt.Errorf("queue name is required")
	}

	if spec.EnableDLQ {
		dlqName := spec.Name + "-dlq"
		dlqArn, err := p.createQueueAndGetArn(ctx, dlqName, nil)
		if err != nil {
			return "", fmt.Errorf("ensure dlq %q: %w", dlqName, err)
		}
		max := spec.DLQMaxReceiveCount
		if max <= 0 {
			max = 5
		}
		redrive, err := json.Marshal(map[string]any{
			"deadLetterTargetArn": dlqArn,
			"maxReceiveCount":     max,
		})
		if err != nil {
			return "", fmt.Errorf("marshal redrive policy: %w", err)
		}
		return p.createQueueAndGetArn(ctx, spec.Name, map[string]string{
			string(sqstypes.QueueAttributeNameRedrivePolicy): string(redrive),
		})
	}

	return p.createQueueAndGetArn(ctx, spec.Name, nil)
}

func (p *Provisioner) createQueueAndGetArn(ctx context.Context, name string, attributes map[string]string) (string, error) {
	// CreateQueue is idempotent on AWS and LocalStack: if the queue exists with
	// the same attributes, it returns 200 with the existing URL. If attributes
	// differ, AWS returns QueueNameExists — we treat that as benign and rely on
	// a follow-up SetQueueAttributes to converge state.
	out, err := p.sqs.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName:  aws.String(name),
		Attributes: attributes,
	})
	if err != nil {
		// Fallback: queue exists with different attributes — look it up.
		urlOut, urlErr := p.sqs.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(name)})
		if urlErr != nil {
			return "", fmt.Errorf("create queue %q: %w", name, err)
		}
		out = &sqs.CreateQueueOutput{QueueUrl: urlOut.QueueUrl}
		if len(attributes) > 0 {
			if _, setErr := p.sqs.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
				QueueUrl:   out.QueueUrl,
				Attributes: attributes,
			}); setErr != nil {
				return "", fmt.Errorf("set attributes on existing queue %q: %w", name, setErr)
			}
		}
	}

	attrOut, err := p.sqs.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       out.QueueUrl,
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return "", fmt.Errorf("get attributes for queue %q: %w", name, err)
	}
	arn, ok := attrOut.Attributes[string(sqstypes.QueueAttributeNameQueueArn)]
	if !ok {
		return "", fmt.Errorf("queue %q has no ARN attribute", name)
	}
	return arn, nil
}

// LookupTopicArn returns the ARN of an existing SNS topic, looked up by name
// in the SDK's configured region. Returns an error if the topic does not exist
// — never creates one. Consumers (services that only subscribe to topics owned
// by another service) MUST use this rather than EnsureTopic to avoid the
// silent-failure mode where a typo or deployment-ordering bug would otherwise
// cause them to create a phantom topic and subscribe to it.
func (p *Provisioner) LookupTopicArn(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("topic name is required")
	}

	var nextToken *string
	for {
		out, err := p.sns.ListTopics(ctx, &sns.ListTopicsInput{NextToken: nextToken})
		if err != nil {
			return "", fmt.Errorf("list topics: %w", err)
		}
		for _, t := range out.Topics {
			arn := aws.ToString(t.TopicArn)
			// SNS topic ARNs end in ":<topic-name>". Match by suffix so we don't
			// false-positive on a name that happens to be a substring.
			if hasArnSuffix(arn, name) {
				return arn, nil
			}
		}
		if out.NextToken == nil {
			return "", fmt.Errorf("sns topic %q not found in region", name)
		}
		nextToken = out.NextToken
	}
}

func hasArnSuffix(arn, name string) bool {
	if len(arn) <= len(name) {
		return false
	}
	return arn[len(arn)-len(name)-1] == ':' && arn[len(arn)-len(name):] == name
}

// EnsureTopic creates the SNS topic if missing and returns its ARN.
// CreateTopic is natively idempotent.
func (p *Provisioner) EnsureTopic(ctx context.Context, name string) (topicArn string, err error) {
	if name == "" {
		return "", fmt.Errorf("topic name is required")
	}
	out, err := p.sns.CreateTopic(ctx, &sns.CreateTopicInput{Name: aws.String(name)})
	if err != nil {
		return "", fmt.Errorf("create topic %q: %w", name, err)
	}
	return aws.ToString(out.TopicArn), nil
}

// SubscriptionSpec describes an SNS→SQS subscription.
type SubscriptionSpec struct {
	TopicArn string
	QueueArn string
	// RawMessageDelivery=true matches the terragrunt default: SNS forwards the
	// raw message body without wrapping it in the SNS envelope.
	RawMessageDelivery bool
	// FilterPolicy is an optional SNS subscription filter. When non-nil, it is
	// marshaled to JSON and attached to the subscription.
	FilterPolicy any
	// FilterPolicyScope is the SNS filter policy scope. Use "MessageBody" to
	// filter on the body (e.g. S3 event keys) instead of message attributes.
	FilterPolicyScope string
}

// EnsureSubscription subscribes the queue to the topic. SNS Subscribe is
// idempotent on the (topic, protocol, endpoint) tuple: repeated calls return
// the same SubscriptionArn.
//
// AWS only applies the Attributes map on the initial Subscribe — when the
// subscription already exists, the existing ARN is returned and any new
// attribute values in the input are ignored. To converge on reruns (e.g.
// when RawMessageDelivery, FilterPolicy, or FilterPolicyScope change), each
// desired attribute is re-applied via SetSubscriptionAttributes after the
// Subscribe call.
func (p *Provisioner) EnsureSubscription(ctx context.Context, spec SubscriptionSpec) (subscriptionArn string, err error) {
	if spec.TopicArn == "" || spec.QueueArn == "" {
		return "", fmt.Errorf("topic ARN and queue ARN are required")
	}

	attributes := map[string]string{}
	if spec.RawMessageDelivery {
		attributes["RawMessageDelivery"] = "true"
	}
	if spec.FilterPolicy != nil {
		raw, err := json.Marshal(spec.FilterPolicy)
		if err != nil {
			return "", fmt.Errorf("marshal filter policy: %w", err)
		}
		attributes["FilterPolicy"] = string(raw)
		if spec.FilterPolicyScope != "" {
			attributes["FilterPolicyScope"] = spec.FilterPolicyScope
		}
	}

	out, err := p.sns.Subscribe(ctx, &sns.SubscribeInput{
		TopicArn:              aws.String(spec.TopicArn),
		Protocol:              aws.String("sqs"),
		Endpoint:              aws.String(spec.QueueArn),
		ReturnSubscriptionArn: true,
		Attributes:            attributes,
	})
	if err != nil {
		return "", fmt.Errorf("subscribe %q to %q: %w", spec.QueueArn, spec.TopicArn, err)
	}

	subArn := aws.ToString(out.SubscriptionArn)
	for name, value := range attributes {
		if _, err := p.sns.SetSubscriptionAttributes(ctx, &sns.SetSubscriptionAttributesInput{
			SubscriptionArn: aws.String(subArn),
			AttributeName:   aws.String(name),
			AttributeValue:  aws.String(value),
		}); err != nil {
			return "", fmt.Errorf("set subscription attribute %q on %q: %w", name, subArn, err)
		}
	}
	return subArn, nil
}
