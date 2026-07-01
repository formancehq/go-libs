package awsbroker_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"

	"github.com/formancehq/go-libs/v5/pkg/messaging/awsbroker"
)

func TestEnsureQueue_BasicCreatesAndReturnsArn(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	queueURL := "https://sqs.eu-west-1.amazonaws.com/123/test-queue"
	queueArn := "arn:aws:sqs:eu-west-1:123:test-queue"

	mockSQS.EXPECT().
		CreateQueue(gomock.Any(), gomock.Any()).
		Return(&sqs.CreateQueueOutput{QueueUrl: aws.String(queueURL)}, nil)

	mockSQS.EXPECT().
		GetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(&sqs.GetQueueAttributesOutput{
			Attributes: map[string]string{
				string(sqstypes.QueueAttributeNameQueueArn): queueArn,
			},
		}, nil)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	got, err := p.EnsureQueue(context.Background(), awsbroker.QueueSpec{Name: "test-queue"})
	require.NoError(t, err)
	assert.Equal(t, queueArn, got)
}

func TestEnsureQueue_AttachesResourcePolicyForSingleSourceTopic(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	queueURL := "https://sqs.eu-west-1.amazonaws.com/123/main-queue"
	queueArn := "arn:aws:sqs:eu-west-1:123:main-queue"
	topicArn := "arn:aws:sns:eu-west-1:123:banking-bridge-channels"

	mockSQS.EXPECT().
		CreateQueue(gomock.Any(), gomock.Any()).
		Return(&sqs.CreateQueueOutput{QueueUrl: aws.String(queueURL)}, nil)
	mockSQS.EXPECT().
		GetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(&sqs.GetQueueAttributesOutput{
			Attributes: map[string]string{string(sqstypes.QueueAttributeNameQueueArn): queueArn},
		}, nil)

	mockSQS.EXPECT().
		SetQueueAttributes(gomock.Any(), gomock.AssignableToTypeOf(&sqs.SetQueueAttributesInput{})).
		DoAndReturn(func(_ context.Context, in *sqs.SetQueueAttributesInput, _ ...func(*sqs.Options)) (*sqs.SetQueueAttributesOutput, error) {
			assert.Equal(t, queueURL, aws.ToString(in.QueueUrl))
			raw, ok := in.Attributes[string(sqstypes.QueueAttributeNamePolicy)]
			require.True(t, ok, "Policy attribute must be set")

			var doc map[string]any
			require.NoError(t, json.Unmarshal([]byte(raw), &doc))
			assert.Equal(t, "2012-10-17", doc["Version"])
			stmts := doc["Statement"].([]any)
			require.Len(t, stmts, 1)
			s := stmts[0].(map[string]any)
			assert.Equal(t, "Allow", s["Effect"])
			assert.Equal(t, "sqs:SendMessage", s["Action"])
			assert.Equal(t, queueArn, s["Resource"])
			assert.Equal(t, "sns.amazonaws.com", s["Principal"].(map[string]any)["Service"])
			// Single topic ARN renders as a scalar, not an array.
			cond := s["Condition"].(map[string]any)["ArnEquals"].(map[string]any)
			assert.Equal(t, topicArn, cond["aws:SourceArn"])
			return &sqs.SetQueueAttributesOutput{}, nil
		})

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	got, err := p.EnsureQueue(context.Background(), awsbroker.QueueSpec{
		Name:                   "main-queue",
		AllowedSourceTopicArns: []string{topicArn},
	})
	require.NoError(t, err)
	assert.Equal(t, queueArn, got)
}

func TestEnsureQueue_AttachesResourcePolicyForMultipleSourceTopics(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	queueURL := "https://sqs.eu-west-1.amazonaws.com/123/multi"
	queueArn := "arn:aws:sqs:eu-west-1:123:multi"
	topics := []string{
		"arn:aws:sns:eu-west-1:123:topic-a",
		"arn:aws:sns:eu-west-1:123:topic-b",
	}

	mockSQS.EXPECT().
		CreateQueue(gomock.Any(), gomock.Any()).
		Return(&sqs.CreateQueueOutput{QueueUrl: aws.String(queueURL)}, nil)
	mockSQS.EXPECT().
		GetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(&sqs.GetQueueAttributesOutput{
			Attributes: map[string]string{string(sqstypes.QueueAttributeNameQueueArn): queueArn},
		}, nil)

	mockSQS.EXPECT().
		SetQueueAttributes(gomock.Any(), gomock.AssignableToTypeOf(&sqs.SetQueueAttributesInput{})).
		DoAndReturn(func(_ context.Context, in *sqs.SetQueueAttributesInput, _ ...func(*sqs.Options)) (*sqs.SetQueueAttributesOutput, error) {
			var doc map[string]any
			require.NoError(t, json.Unmarshal([]byte(in.Attributes[string(sqstypes.QueueAttributeNamePolicy)]), &doc))
			stmts := doc["Statement"].([]any)
			cond := stmts[0].(map[string]any)["Condition"].(map[string]any)["ArnEquals"].(map[string]any)
			arns, ok := cond["aws:SourceArn"].([]any)
			require.True(t, ok, "multiple topics must render as a JSON array")
			assert.ElementsMatch(t, []any{topics[0], topics[1]}, arns)
			return &sqs.SetQueueAttributesOutput{}, nil
		})

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	_, err := p.EnsureQueue(context.Background(), awsbroker.QueueSpec{
		Name:                   "multi",
		AllowedSourceTopicArns: topics,
	})
	require.NoError(t, err)
}

func TestEnsureQueue_OmitsPolicyWhenNoSourceTopics(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	mockSQS.EXPECT().
		CreateQueue(gomock.Any(), gomock.Any()).
		Return(&sqs.CreateQueueOutput{QueueUrl: aws.String("u")}, nil)
	mockSQS.EXPECT().
		GetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(&sqs.GetQueueAttributesOutput{
			Attributes: map[string]string{string(sqstypes.QueueAttributeNameQueueArn): "arn:q"},
		}, nil)
	// No SetQueueAttributes call is expected — gomock will fail the test if one occurs.

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	_, err := p.EnsureQueue(context.Background(), awsbroker.QueueSpec{Name: "q"})
	require.NoError(t, err)
}

func TestEnsureQueue_PolicyAttachedOnlyToMainQueueNotDLQ(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	dlqURL := "https://sqs.eu-west-1.amazonaws.com/123/q-dlq"
	dlqArn := "arn:aws:sqs:eu-west-1:123:q-dlq"
	mainURL := "https://sqs.eu-west-1.amazonaws.com/123/q"
	mainArn := "arn:aws:sqs:eu-west-1:123:q"

	// DLQ first (no attributes).
	mockSQS.EXPECT().
		CreateQueue(gomock.Any(), gomock.AssignableToTypeOf(&sqs.CreateQueueInput{})).
		DoAndReturn(func(_ context.Context, in *sqs.CreateQueueInput, _ ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error) {
			assert.Equal(t, "q-dlq", aws.ToString(in.QueueName))
			return &sqs.CreateQueueOutput{QueueUrl: aws.String(dlqURL)}, nil
		})
	mockSQS.EXPECT().
		GetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(&sqs.GetQueueAttributesOutput{
			Attributes: map[string]string{string(sqstypes.QueueAttributeNameQueueArn): dlqArn},
		}, nil)
	// Main queue with redrive.
	mockSQS.EXPECT().
		CreateQueue(gomock.Any(), gomock.AssignableToTypeOf(&sqs.CreateQueueInput{})).
		DoAndReturn(func(_ context.Context, in *sqs.CreateQueueInput, _ ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error) {
			assert.Equal(t, "q", aws.ToString(in.QueueName))
			_, ok := in.Attributes[string(sqstypes.QueueAttributeNameRedrivePolicy)]
			assert.True(t, ok, "main queue must carry redrive policy")
			return &sqs.CreateQueueOutput{QueueUrl: aws.String(mainURL)}, nil
		})
	mockSQS.EXPECT().
		GetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(&sqs.GetQueueAttributesOutput{
			Attributes: map[string]string{string(sqstypes.QueueAttributeNameQueueArn): mainArn},
		}, nil)
	// Exactly one SetQueueAttributes call — on the main queue, for the Policy.
	mockSQS.EXPECT().
		SetQueueAttributes(gomock.Any(), gomock.AssignableToTypeOf(&sqs.SetQueueAttributesInput{})).
		DoAndReturn(func(_ context.Context, in *sqs.SetQueueAttributesInput, _ ...func(*sqs.Options)) (*sqs.SetQueueAttributesOutput, error) {
			assert.Equal(t, mainURL, aws.ToString(in.QueueUrl), "policy must target main queue, never the DLQ")
			_, ok := in.Attributes[string(sqstypes.QueueAttributeNamePolicy)]
			assert.True(t, ok)
			return &sqs.SetQueueAttributesOutput{}, nil
		})

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	got, err := p.EnsureQueue(context.Background(), awsbroker.QueueSpec{
		Name:                   "q",
		EnableDLQ:              true,
		AllowedSourceTopicArns: []string{"arn:aws:sns:eu-west-1:123:topic"},
	})
	require.NoError(t, err)
	assert.Equal(t, mainArn, got)
}

func TestEnsureQueue_PropagatesPolicyAttachError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	mockSQS.EXPECT().
		CreateQueue(gomock.Any(), gomock.Any()).
		Return(&sqs.CreateQueueOutput{QueueUrl: aws.String("u")}, nil)
	mockSQS.EXPECT().
		GetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(&sqs.GetQueueAttributesOutput{
			Attributes: map[string]string{string(sqstypes.QueueAttributeNameQueueArn): "arn:q"},
		}, nil)

	boom := errors.New("policy-boom")
	mockSQS.EXPECT().
		SetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(nil, boom)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	_, err := p.EnsureQueue(context.Background(), awsbroker.QueueSpec{
		Name:                   "q",
		AllowedSourceTopicArns: []string{"arn:topic"},
	})
	require.ErrorIs(t, err, boom)
}

func TestEnsureQueue_WithDLQAttachesRedrivePolicy(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	dlqURL := "https://sqs.eu-west-1.amazonaws.com/123/test-queue-dlq"
	dlqArn := "arn:aws:sqs:eu-west-1:123:test-queue-dlq"
	mainURL := "https://sqs.eu-west-1.amazonaws.com/123/test-queue"
	mainArn := "arn:aws:sqs:eu-west-1:123:test-queue"

	// 1) DLQ is created first, with no attributes.
	mockSQS.EXPECT().
		CreateQueue(gomock.Any(), gomock.AssignableToTypeOf(&sqs.CreateQueueInput{})).
		DoAndReturn(func(_ context.Context, in *sqs.CreateQueueInput, _ ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error) {
			assert.Equal(t, "test-queue-dlq", aws.ToString(in.QueueName))
			assert.Empty(t, in.Attributes, "DLQ should have no attributes")
			return &sqs.CreateQueueOutput{QueueUrl: aws.String(dlqURL)}, nil
		})
	mockSQS.EXPECT().
		GetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(&sqs.GetQueueAttributesOutput{
			Attributes: map[string]string{string(sqstypes.QueueAttributeNameQueueArn): dlqArn},
		}, nil)

	// 2) Then the main queue with a RedrivePolicy referencing the DLQ.
	mockSQS.EXPECT().
		CreateQueue(gomock.Any(), gomock.AssignableToTypeOf(&sqs.CreateQueueInput{})).
		DoAndReturn(func(_ context.Context, in *sqs.CreateQueueInput, _ ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error) {
			assert.Equal(t, "test-queue", aws.ToString(in.QueueName))
			raw, ok := in.Attributes[string(sqstypes.QueueAttributeNameRedrivePolicy)]
			require.True(t, ok, "RedrivePolicy attribute must be set")
			var policy map[string]any
			require.NoError(t, json.Unmarshal([]byte(raw), &policy))
			assert.Equal(t, dlqArn, policy["deadLetterTargetArn"])
			assert.EqualValues(t, 3, policy["maxReceiveCount"])
			return &sqs.CreateQueueOutput{QueueUrl: aws.String(mainURL)}, nil
		})
	mockSQS.EXPECT().
		GetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(&sqs.GetQueueAttributesOutput{
			Attributes: map[string]string{string(sqstypes.QueueAttributeNameQueueArn): mainArn},
		}, nil)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	got, err := p.EnsureQueue(context.Background(), awsbroker.QueueSpec{
		Name:               "test-queue",
		EnableDLQ:          true,
		DLQMaxReceiveCount: 3,
	})
	require.NoError(t, err)
	assert.Equal(t, mainArn, got)
}

func TestEnsureQueue_DefaultDLQMaxReceiveIsFive(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	mockSQS.EXPECT().
		CreateQueue(gomock.Any(), gomock.Any()).
		Return(&sqs.CreateQueueOutput{QueueUrl: aws.String("u")}, nil)
	mockSQS.EXPECT().
		GetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(&sqs.GetQueueAttributesOutput{
			Attributes: map[string]string{string(sqstypes.QueueAttributeNameQueueArn): "arn:dlq"},
		}, nil)
	mockSQS.EXPECT().
		CreateQueue(gomock.Any(), gomock.AssignableToTypeOf(&sqs.CreateQueueInput{})).
		DoAndReturn(func(_ context.Context, in *sqs.CreateQueueInput, _ ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error) {
			var policy map[string]any
			require.NoError(t, json.Unmarshal([]byte(in.Attributes[string(sqstypes.QueueAttributeNameRedrivePolicy)]), &policy))
			assert.EqualValues(t, 5, policy["maxReceiveCount"], "default must be 5")
			return &sqs.CreateQueueOutput{QueueUrl: aws.String("u2")}, nil
		})
	mockSQS.EXPECT().
		GetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(&sqs.GetQueueAttributesOutput{
			Attributes: map[string]string{string(sqstypes.QueueAttributeNameQueueArn): "arn:main"},
		}, nil)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	_, err := p.EnsureQueue(context.Background(), awsbroker.QueueSpec{
		Name:      "q",
		EnableDLQ: true,
		// DLQMaxReceiveCount left at zero — default expected.
	})
	require.NoError(t, err)
}

func TestEnsureQueue_RejectsEmptyName(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	_, err := p.EnsureQueue(context.Background(), awsbroker.QueueSpec{Name: ""})
	require.Error(t, err)
}

func TestEnsureQueue_FallsBackToGetQueueUrlWhenCreateFails(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	existingURL := "https://sqs.eu-west-1.amazonaws.com/123/existing-queue"
	existingArn := "arn:aws:sqs:eu-west-1:123:existing-queue"

	// First CreateQueue fails with "queue exists with different attributes" —
	// the provisioner falls back to GetQueueUrl + SetQueueAttributes when there
	// are attributes to converge, then GetQueueAttributes to return the ARN.
	// We use EnableDLQ:true so the main queue carries a RedrivePolicy that must
	// be re-applied via SetQueueAttributes.
	dlqURL := "https://sqs.eu-west-1.amazonaws.com/123/existing-queue-dlq"
	dlqArn := "arn:aws:sqs:eu-west-1:123:existing-queue-dlq"

	// DLQ creation succeeds normally.
	mockSQS.EXPECT().
		CreateQueue(gomock.Any(), gomock.Any()).
		Return(&sqs.CreateQueueOutput{QueueUrl: aws.String(dlqURL)}, nil)
	mockSQS.EXPECT().
		GetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(&sqs.GetQueueAttributesOutput{
			Attributes: map[string]string{string(sqstypes.QueueAttributeNameQueueArn): dlqArn},
		}, nil)
	// Main queue: CreateQueue fails (already exists with different attributes).
	mockSQS.EXPECT().
		CreateQueue(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("QueueNameExists"))
	mockSQS.EXPECT().
		GetQueueUrl(gomock.Any(), gomock.Any()).
		Return(&sqs.GetQueueUrlOutput{QueueUrl: aws.String(existingURL)}, nil)
	mockSQS.EXPECT().
		SetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(&sqs.SetQueueAttributesOutput{}, nil)
	mockSQS.EXPECT().
		GetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(&sqs.GetQueueAttributesOutput{
			Attributes: map[string]string{string(sqstypes.QueueAttributeNameQueueArn): existingArn},
		}, nil)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	got, err := p.EnsureQueue(context.Background(), awsbroker.QueueSpec{
		Name:      "existing-queue",
		EnableDLQ: true,
	})
	require.NoError(t, err)
	assert.Equal(t, existingArn, got)
}

func TestEnsureTopic_BasicCreatesAndReturnsArn(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	wantArn := "arn:aws:sns:eu-west-1:123:my-topic"
	mockSNS.EXPECT().
		CreateTopic(gomock.Any(), gomock.AssignableToTypeOf(&sns.CreateTopicInput{})).
		DoAndReturn(func(_ context.Context, in *sns.CreateTopicInput, _ ...func(*sns.Options)) (*sns.CreateTopicOutput, error) {
			assert.Equal(t, "my-topic", aws.ToString(in.Name))
			return &sns.CreateTopicOutput{TopicArn: aws.String(wantArn)}, nil
		})

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	got, err := p.EnsureTopic(context.Background(), "my-topic")
	require.NoError(t, err)
	assert.Equal(t, wantArn, got)
}

func TestEnsureTopic_RejectsEmptyName(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	_, err := p.EnsureTopic(context.Background(), "")
	require.Error(t, err)
}

func TestEnsureSubscription_PassesRawDeliveryAndFilterPolicy(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	wantSubArn := "arn:aws:sns:eu-west-1:123:topic:sub-id"
	filter := map[string]any{
		"Records": map[string]any{
			"s3": map[string]any{
				"object": map[string]any{
					"key": []map[string]string{{"prefix": "ebics/incoming/pending/"}},
				},
			},
		},
	}

	mockSNS.EXPECT().
		Subscribe(gomock.Any(), gomock.AssignableToTypeOf(&sns.SubscribeInput{})).
		DoAndReturn(func(_ context.Context, in *sns.SubscribeInput, _ ...func(*sns.Options)) (*sns.SubscribeOutput, error) {
			assert.Equal(t, "arn:aws:sns:eu-west-1:123:topic", aws.ToString(in.TopicArn))
			assert.Equal(t, "sqs", aws.ToString(in.Protocol))
			assert.Equal(t, "arn:aws:sqs:eu-west-1:123:q", aws.ToString(in.Endpoint))
			assert.True(t, in.ReturnSubscriptionArn)
			assert.Equal(t, "true", in.Attributes["RawMessageDelivery"])
			assert.Equal(t, "MessageBody", in.Attributes["FilterPolicyScope"])

			var gotFilter map[string]any
			require.NoError(t, json.Unmarshal([]byte(in.Attributes["FilterPolicy"]), &gotFilter))
			// JSON unmarshal coerces nested types but the top-level keys must match.
			assert.Contains(t, gotFilter, "Records")

			return &sns.SubscribeOutput{SubscriptionArn: aws.String(wantSubArn)}, nil
		})

	// Convergence step: each attribute is re-applied via
	// SetSubscriptionAttributes so reruns enforce the latest values even when
	// AWS returned a pre-existing subscription ARN.
	gotAttrs := map[string]string{}
	mockSNS.EXPECT().
		SetSubscriptionAttributes(gomock.Any(), gomock.AssignableToTypeOf(&sns.SetSubscriptionAttributesInput{})).
		DoAndReturn(func(_ context.Context, in *sns.SetSubscriptionAttributesInput, _ ...func(*sns.Options)) (*sns.SetSubscriptionAttributesOutput, error) {
			assert.Equal(t, wantSubArn, aws.ToString(in.SubscriptionArn))
			gotAttrs[aws.ToString(in.AttributeName)] = aws.ToString(in.AttributeValue)
			return &sns.SetSubscriptionAttributesOutput{}, nil
		}).
		Times(3)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	got, err := p.EnsureSubscription(context.Background(), awsbroker.SubscriptionSpec{
		TopicArn:           "arn:aws:sns:eu-west-1:123:topic",
		QueueArn:           "arn:aws:sqs:eu-west-1:123:q",
		RawMessageDelivery: true,
		FilterPolicy:       filter,
		FilterPolicyScope:  "MessageBody",
	})
	require.NoError(t, err)
	assert.Equal(t, wantSubArn, got)
	assert.Equal(t, "true", gotAttrs["RawMessageDelivery"])
	assert.Equal(t, "MessageBody", gotAttrs["FilterPolicyScope"])
	assert.Contains(t, gotAttrs, "FilterPolicy")
}

func TestEnsureSubscription_WithoutFilterPolicyOmitsAttribute(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	mockSNS.EXPECT().
		Subscribe(gomock.Any(), gomock.AssignableToTypeOf(&sns.SubscribeInput{})).
		DoAndReturn(func(_ context.Context, in *sns.SubscribeInput, _ ...func(*sns.Options)) (*sns.SubscribeOutput, error) {
			_, hasFilter := in.Attributes["FilterPolicy"]
			assert.False(t, hasFilter, "FilterPolicy must be absent when not configured")
			return &sns.SubscribeOutput{SubscriptionArn: aws.String("arn:sub")}, nil
		})

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	_, err := p.EnsureSubscription(context.Background(), awsbroker.SubscriptionSpec{
		TopicArn: "arn:aws:sns:eu-west-1:123:topic",
		QueueArn: "arn:aws:sqs:eu-west-1:123:q",
	})
	require.NoError(t, err)
}

func TestEnsureSubscription_RejectsEmptyArns(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)
	p := awsbroker.NewProvisioner(mockSQS, mockSNS)

	_, err := p.EnsureSubscription(context.Background(), awsbroker.SubscriptionSpec{
		TopicArn: "",
		QueueArn: "arn:q",
	})
	require.Error(t, err)

	_, err = p.EnsureSubscription(context.Background(), awsbroker.SubscriptionSpec{
		TopicArn: "arn:topic",
		QueueArn: "",
	})
	require.Error(t, err)
}

func TestEnsureSubscription_PropagatesSubscribeError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	boom := errors.New("boom")
	mockSNS.EXPECT().
		Subscribe(gomock.Any(), gomock.Any()).
		Return(nil, boom)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	_, err := p.EnsureSubscription(context.Background(), awsbroker.SubscriptionSpec{
		TopicArn: "arn:topic",
		QueueArn: "arn:queue",
	})
	require.ErrorIs(t, err, boom)
}

func TestEnsureSubscription_PropagatesSetAttributesError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	mockSNS.EXPECT().
		Subscribe(gomock.Any(), gomock.Any()).
		Return(&sns.SubscribeOutput{SubscriptionArn: aws.String("arn:sub")}, nil)

	boom := errors.New("set-attr-boom")
	mockSNS.EXPECT().
		SetSubscriptionAttributes(gomock.Any(), gomock.Any()).
		Return(nil, boom)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	_, err := p.EnsureSubscription(context.Background(), awsbroker.SubscriptionSpec{
		TopicArn:           "arn:topic",
		QueueArn:           "arn:queue",
		RawMessageDelivery: true,
	})
	require.ErrorIs(t, err, boom)
}

func TestEnsureQueue_MissingArnAttributeReturnsError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	mockSQS.EXPECT().
		CreateQueue(gomock.Any(), gomock.Any()).
		Return(&sqs.CreateQueueOutput{QueueUrl: aws.String("u")}, nil)
	mockSQS.EXPECT().
		GetQueueAttributes(gomock.Any(), gomock.Any()).
		Return(&sqs.GetQueueAttributesOutput{Attributes: map[string]string{}}, nil)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	_, err := p.EnsureQueue(context.Background(), awsbroker.QueueSpec{Name: "q"})
	require.Error(t, err)
}

func TestLookupTopicArn_FoundOnFirstPage(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	want := "arn:aws:sns:eu-west-1:123:banking-bridge-channels"
	mockSNS.EXPECT().
		ListTopics(gomock.Any(), gomock.Any()).
		Return(&sns.ListTopicsOutput{
			Topics: []snstypes.Topic{
				{TopicArn: aws.String("arn:aws:sns:eu-west-1:123:something-else")},
				{TopicArn: aws.String(want)},
			},
		}, nil)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	got, err := p.LookupTopicArn(context.Background(), "banking-bridge-channels")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestLookupTopicArn_PaginatesUntilFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	want := "arn:aws:sns:eu-west-1:123:target"
	gomock.InOrder(
		mockSNS.EXPECT().
			ListTopics(gomock.Any(), gomock.Any()).
			Return(&sns.ListTopicsOutput{
				Topics:    []snstypes.Topic{{TopicArn: aws.String("arn:aws:sns:eu-west-1:123:other")}},
				NextToken: aws.String("token-1"),
			}, nil),
		mockSNS.EXPECT().
			ListTopics(gomock.Any(), gomock.Any()).
			Return(&sns.ListTopicsOutput{
				Topics: []snstypes.Topic{{TopicArn: aws.String(want)}},
			}, nil),
	)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	got, err := p.LookupTopicArn(context.Background(), "target")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestLookupTopicArn_NotFoundReturnsError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	mockSNS.EXPECT().
		ListTopics(gomock.Any(), gomock.Any()).
		Return(&sns.ListTopicsOutput{
			Topics: []snstypes.Topic{{TopicArn: aws.String("arn:aws:sns:eu-west-1:123:nope")}},
		}, nil)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	_, err := p.LookupTopicArn(context.Background(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

func TestLookupTopicArn_RejectsEmptyName(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	_, err := p.LookupTopicArn(context.Background(), "")
	require.Error(t, err)
}

func TestLookupTopicArn_PropagatesListError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	boom := errors.New("boom")
	mockSNS.EXPECT().
		ListTopics(gomock.Any(), gomock.Any()).
		Return(nil, boom)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	_, err := p.LookupTopicArn(context.Background(), "any")
	require.ErrorIs(t, err, boom)
}

func TestLookupTopicArn_RejectsSubstringFalsePositive(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockSQS := awsbroker.NewMockSQSAPI(ctrl)
	mockSNS := awsbroker.NewMockSNSAPI(ctrl)

	// "channels-update-success" contains "channels" as a substring, but the
	// suffix match against ":channels" must reject it.
	mockSNS.EXPECT().
		ListTopics(gomock.Any(), gomock.Any()).
		Return(&sns.ListTopicsOutput{
			Topics: []snstypes.Topic{
				{TopicArn: aws.String("arn:aws:sns:eu-west-1:123:banking-bridge-channels-update-success")},
			},
		}, nil)

	p := awsbroker.NewProvisioner(mockSQS, mockSNS)
	_, err := p.LookupTopicArn(context.Background(), "channels")
	require.Error(t, err)
}
