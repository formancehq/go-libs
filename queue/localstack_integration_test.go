package queue_test

import (
	"context"
	"crypto/md5" //nolint: gosec
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/ThreeDotsLabs/watermill-aws/sqs"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	sqsservice "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/elgohr/go-localstack"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomegatypes "github.com/onsi/gomega/types"

	"github.com/formancehq/go-libs/v4/logging"
	"github.com/formancehq/go-libs/v4/queue"
)

func TestListener(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Listener Suite")
}

func initClient() (client *sqsservice.Client, queueURL string, ch <-chan *message.Message, err error) {
	ctx := logging.TestingContext()
	fromEnvOpt, err := localstack.WithClientFromEnv()
	if err != nil {
		return nil, "", ch, fmt.Errorf("Could not connect to Docker %w", err)
	}
	l, err := localstack.NewInstance(fromEnvOpt)
	if err != nil {
		return nil, "", ch, fmt.Errorf("Could not connect to Docker %w", err)
	}
	if err := l.Start(); err != nil {
		return nil, "", ch, fmt.Errorf("Could not start localstack %w", err)
	}
	DeferCleanup(l.Stop)

	region := "us-east-1"
	endpoint := l.EndpointV2(localstack.SQS)

	//nolint: staticcheck
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:           l.EndpointV2(localstack.SQS),
					SigningRegion: region,
				}, nil
			},
		)),
	)
	if err != nil {
		return nil, "", ch, fmt.Errorf("Could not load config %w", err)
	}
	optFns := []func(o *sqsservice.Options){
		func(o *sqsservice.Options) {
			o.BaseEndpoint = &endpoint
			o.Credentials = credentials.NewStaticCredentialsProvider("dummy", "dummy", "dummy")
		},
	}

	subscriber, err := sqs.NewSubscriber(sqs.SubscriberConfig{
		AWSConfig: cfg,
		OptFns:    optFns,
	}, nil)
	if err != nil {
		return nil, "", ch, fmt.Errorf("Failed to create subscriber %w", err)
	}

	client = sqsservice.NewFromConfig(cfg, optFns...)
	queueName := uuid.NewString()
	out, err := client.CreateQueue(ctx, &sqsservice.CreateQueueInput{
		QueueName: &queueName,
	})
	if err != nil {
		return nil, "", ch, fmt.Errorf("Could not create queue %w", err)
	}
	ch, err = subscriber.Subscribe(ctx, queueName)
	if err != nil {
		return nil, "", ch, fmt.Errorf("Failed to subscribe %w", err)
	}
	return client, *out.QueueUrl, ch, nil
}

var _ = Describe("SQS Listener", func() {
	var (
		client   *sqsservice.Client
		ch       <-chan *message.Message
		queueURL string
		logger   logging.Logger
		err      error
	)

	Context("listening", func() {
		BeforeEach(func() {
			client, queueURL, ch, err = initClient()
			Expect(err).To(BeNil())

			logger = logging.NewDefaultLogger(GinkgoWriter, true, true, false)
		})

		It("calls the callback function for each message", func(specCtx SpecContext) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			metadataTagKey := "some-key"
			expectedValue := "the-value"
			var callbackCount int
			var foundKey bool
			var foundValue string
			out := make(chan string)
			fn := func(ctx context.Context, meta map[string]string, msg []byte) error {
				callbackCount++
				foundValue, foundKey = meta[metadataTagKey]
				out <- string(msg)
				return nil
			}
			listener, err := queue.NewListener(logger, fn, 2)
			Expect(err).To(BeNil())
			DeferCleanup(listener.Done)
			listener.Listen(ctx, ch)

			body := "hi"
			res, err := client.SendMessage(specCtx, &sqsservice.SendMessageInput{
				QueueUrl:    &queueURL,
				MessageBody: &body,
				MessageAttributes: map[string]types.MessageAttributeValue{
					metadataTagKey: {
						DataType:    aws.String("String"),
						StringValue: aws.String(expectedValue),
					},
				},
			})
			Expect(err).To(BeNil())
			Eventually(out).Should(Receive(Message(*res.MD5OfMessageBody)))
			Expect(callbackCount).To(Equal(1))
			Expect(foundKey).To(BeTrue())
			Expect(foundValue).To(Equal(expectedValue))
		})

		It("closes even if listen has never been called", func(specCtx SpecContext) {
			fn := func(ctx context.Context, meta map[string]string, msg []byte) error {
				return nil
			}
			listener, err := queue.NewListener(logger, fn, 2)
			Expect(err).To(BeNil())
			DeferCleanup(listener.Done)
		})
	})
})

type MessageMatcher struct {
	md5OfBody string
	err       error
}

func (m *MessageMatcher) Match(actual any) (success bool, err error) {
	msg, ok := actual.(string)
	if !ok {
		m.err = fmt.Errorf("expected type string but got %T", actual)
		return false, nil
	}

	//nolint: gosec
	hash := md5.Sum([]byte(msg))
	sum := hex.EncodeToString(hash[:])
	if sum != m.md5OfBody {
		m.err = fmt.Errorf("expected: %q, got: %q", m.md5OfBody, sum)
		return false, nil
	}
	return true, nil
}

func (m *MessageMatcher) FailureMessage(_ any) (message string) {
	return fmt.Sprintf("message does not match expectations: %s", m.err)
}

func (m *MessageMatcher) NegatedFailureMessage(_ any) (message string) {
	return "message should not match"
}

var _ gomegatypes.GomegaMatcher = (*MessageMatcher)(nil)

func Message(md5OfBody string) gomegatypes.GomegaMatcher {
	return &MessageMatcher{
		md5OfBody: md5OfBody,
	}
}
