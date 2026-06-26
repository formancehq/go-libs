package connect

import (
	"context"
	"database/sql/driver"
	"fmt"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/pkg/errors"
	"github.com/xo/dburl"
	"go.opentelemetry.io/otel"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

var tracer = otel.Tracer("github.com/formancehq/go-libs/v5/pkg/storage/bun/connect")

type iamDriver struct {
	awsConfig aws.Config
}

func (driver *iamDriver) OpenConnector(name string) (driver.Connector, error) {
	return &iamConnector{
		dsn:    name,
		driver: driver,
	}, nil
}

func (driver *iamDriver) Open(name string) (driver.Conn, error) {
	connector, err := driver.OpenConnector(name)
	if err != nil {
		return nil, err
	}
	return connector.Connect(context.Background())
}

var _ driver.Driver = &iamDriver{}
var _ driver.DriverContext = &iamDriver{}

type iamConnector struct {
	dsn     string
	driver  *iamDriver
	logger  logging.Logger
	options []Option
}

func (i *iamConnector) Connect(ctx context.Context) (driver.Conn, error) {
	databaseURL, err := dburl.Parse(i.dsn)
	if err != nil {
		return nil, errors.New("parsing dsn")
	}

	authenticationToken, err := buildIAMAuthToken(ctx, i.driver.awsConfig, databaseURL.Host, databaseURL.User.Username())
	if err != nil {
		return nil, errors.Wrap(err, "building aws auth token")
	}

	dsn := buildIAMAuthDSN(&databaseURL.URL, authenticationToken)

	i.logger.Debugf("IAM: Connect using dsn '%s'", obfuscateDSN(dsn))

	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dsn: %w", err)
	}
	for _, opt := range i.options {
		opt(config)
	}

	return buildPGXConnector(config).Connect(ctx)
}

func (i iamConnector) Driver() driver.Driver {
	return &iamDriver{}
}

var _ driver.Connector = &iamConnector{}

func buildIAMAuthDSN(databaseURL *url.URL, authenticationToken string) string {
	dsnURL := *databaseURL
	dsnURL.User = url.UserPassword(databaseURL.User.Username(), authenticationToken)
	return dsnURL.String()
}
