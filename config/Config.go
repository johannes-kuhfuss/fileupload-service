package config

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-sanitize/sanitize"
	"github.com/johannes-kuhfuss/fileupload-service/domain"
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type AppConfig struct {
	Server struct {
		Host                 string `envconfig:"SERVER_HOST"`
		Port                 string `envconfig:"SERVER_PORT" default:"8080"`
		TlsPort              string `envconfig:"SERVER_TLS_PORT" default:"8443"`
		GracefulShutdownTime int    `envconfig:"GRACEFUL_SHUTDOWN_TIME" default:"10"`
		UseTls               bool   `envconfig:"USE_TLS" default:"false"`
		CertFile             string `envconfig:"CERT_FILE" default:"./cert/cert.pem"`
		KeyFile              string `envconfig:"KEY_FILE" default:"./cert/cert.key"`
	}
	Gin struct {
		Mode         string `envconfig:"GIN_MODE" default:"release"`
		TemplatePath string `envconfig:"TEMPLATE_PATH" default:"./templates/"`
	}
	Upload struct {
		Path              string   `envconfig:"UPLOAD_PATH" default:"C:\\TEMP"`
		AllowedExtensions []string `envconfig:"ALLOWED_EXTENSIONS" default:".mp3,.m4a,.wav"`
	}
	RunTime struct {
		Router     *gin.Engine
		ListenAddr string
		StartDate  time.Time
		Sani       *sanitize.Sanitizer
		UploadList []domain.Upload
		OTrace     trace.Tracer
		OMeter     metric.Meter
		OLog       *slog.Logger
	}
	Metrics struct {
		UploadSuccessCounter metric.Int64Counter
		UploadFailureCounter metric.Int64Counter
	}
}

var (
	EnvFile = ".env"
)

func InitConfig(file string, config *AppConfig) error {
	config.RunTime.OLog.Info(fmt.Sprintf("Initalizing configuration from file %v...", file))
	loadConfig(file)
	err := envconfig.Process("", config)
	if err != nil {
		return fmt.Errorf("Could not initalize configuration. Check your environment variables. %v", err.Error())
	}
	config.RunTime.OLog.Info("Configuration initialized")
	return nil
}

func loadConfig(file string) error {
	err := godotenv.Load(file)
	if err != nil {
		fmt.Println("Could not open env file. Using Environment variable and defaults")
		return err
	}
	return nil
}
