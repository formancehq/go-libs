package bunconnect

import (
	"context"
	"database/sql/driver"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pkg/errors"
	"github.com/xo/dburl"
	"go.opentelemetry.io/otel"

	"github.com/formancehq/go-libs/v4/logging"
	"github.com/formancehq/go-libs/v4/otlp"
)

var tracer = otel.Tracer("github.com/formancehq/go-libs/v4/bun/bunconnect")

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
	url, err := dburl.Parse(i.dsn)
	if err != nil {
		return nil, errors.Wrapf(err, "parsing dsn: %s", i.dsn)
	}

	authenticationToken, err := func() (string, error) {
		ctx, span := tracer.Start(ctx, "iam.build-auth-token")
		defer span.End()

		ret, err := auth.BuildAuthToken(
			ctx,
			url.Host,
			i.driver.awsConfig.Region,
			url.User.Username(),
			i.driver.awsConfig.Credentials,
		)
		if err != nil {
			otlp.RecordError(ctx, err)
		}

		return ret, nil
	}()
	if err != nil {
		return nil, errors.Wrap(err, "building aws auth token")
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s",
		url.Hostname(),
		url.Port(),
		url.User.Username(),
		authenticationToken,
		url.Path[1:],
	)
	for key, strings := range url.Query() {
		for _, value := range strings {
			dsn = fmt.Sprintf("%s %s=%s", dsn, key, value)
		}
	}

	i.logger.Debugf("IAM: Connect using dsn '%s'", dsn)

	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dsn: %w", err)
	}
	for _, opt := range i.options {
		opt(config)
	}

	config.Tracer = newPgxTracer()

	return stdlib.GetConnector(*config).Connect(ctx)
}

func (i iamConnector) Driver() driver.Driver {
	return &iamDriver{}
}

var _ driver.Connector = &iamConnector{}
