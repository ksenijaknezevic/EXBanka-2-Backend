// Package transport manages RabbitMQ consumption for notification-service.
package transport

import (
	"encoding/json"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"

	"banka-backend/services/notification-service/internal/config"
	"banka-backend/services/notification-service/internal/domain"
	"banka-backend/services/notification-service/internal/service"
)

const emailQueue = "email_notifications"

// StartConsumer dials RabbitMQ, declares the queue, and begins consuming
// EmailEvent messages. It is designed to be called as a goroutine from main.
// It blocks until the connection is closed.
func StartConsumer(cfg *config.Config, emailSvc *service.EmailService) {
	conn, err := amqp.Dial(cfg.RabbitMQURL)
	if err != nil {
		log.Fatalf("[rabbitmq] failed to connect: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("[rabbitmq] failed to open channel: %v", err)
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(
		emailQueue,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		log.Fatalf("[rabbitmq] failed to declare queue: %v", err)
	}

	msgs, err := ch.Consume(
		emailQueue,
		"",    // consumer tag (auto-generated)
		false, // auto-ack — we ack manually
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		log.Fatalf("[rabbitmq] failed to register consumer: %v", err)
	}

	log.Printf("[rabbitmq] consumer started, waiting for messages on queue %q", emailQueue)

	go func() {
		for msg := range msgs {
			var event domain.EmailEvent
			if err := json.Unmarshal(msg.Body, &event); err != nil {
				log.Printf("[rabbitmq] failed to unmarshal message: %v", err)
				msg.Ack(false) // discard malformed message so it doesn't block the queue
				continue
			}

			if err := emailSvc.SendEmail(event); err != nil {
				log.Printf("[rabbitmq] failed to send email to %s (type=%s): %v", event.Email, event.Type, err)
			} else {
				log.Printf("[rabbitmq] email sent to %s (type=%s)", event.Email, event.Type)
			}

			msg.Ack(false)
		}
	}()

	// Block until the broker closes the connection.
	connErr := <-conn.NotifyClose(make(chan *amqp.Error, 1))
	if connErr != nil {
		log.Printf("[rabbitmq] connection closed: %v", connErr)
	}
}
