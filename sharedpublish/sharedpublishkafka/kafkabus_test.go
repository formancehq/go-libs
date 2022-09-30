package sharedpublishkafka

import (
	"log"
	"os"
	"testing"

	"github.com/Shopify/sarama"
	"github.com/ThreeDotsLabs/watermill-kafka/v2/pkg/kafka"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/assert"
	"go.formance.com/go-libs/sharedpublish"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestModuleKafka(t *testing.T) {

	sarama.Logger = log.New(os.Stdout, "[Sarama] ", log.LstdFlags)

	broker := sarama.NewMockBroker(t, 12)
	defer broker.Close()

	metadataResponse := new(sarama.MetadataResponse)
	metadataResponse.AddBroker(broker.Addr(), broker.BrokerID())
	broker.Returns(metadataResponse)

	app := fxtest.New(t,
		sharedpublish.Module(),
		Module("foo", broker.Addr()),
		fx.Replace(sarama.MinVersion),
		fx.Invoke(func(p message.Publisher) {
			assert.IsType(t, &kafka.Publisher{}, p)
		}),
		ProvideSaramaOption(
			WithProducerReturnSuccess(),
			WithConsumerReturnErrors(),
		),
	)
	app.
		RequireStart().
		RequireStop()
}
