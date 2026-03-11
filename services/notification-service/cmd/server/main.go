// notification-service entrypoint.
//
// Connects to RabbitMQ and consumes EmailEvent messages,
// dispatching HTML emails via SMTP for each event received.
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"banka-backend/services/notification-service/internal/config"
	"banka-backend/services/notification-service/internal/service"
	"banka-backend/services/notification-service/internal/transport"
)

func main() {
	cfg := config.LoadConfig()

	emailSvc := service.NewEmailService(cfg)

	go transport.StartConsumer(cfg, emailSvc)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[main] shutdown signal received, exiting")
}
