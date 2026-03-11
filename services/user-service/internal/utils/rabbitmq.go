// Package utils — RabbitMQ publisher for async email notifications.
package utils

import (
	"encoding/json"

	amqp "github.com/rabbitmq/amqp091-go"
)

const emailQueue = "email_notifications"

// EmailEvent is the message payload published to the email_notifications queue.
// The Notification Service consumes this and dispatches the appropriate email.
type EmailEvent struct {
	Type  string `json:"type"`  // "ACTIVATION" | "RESET"
	Email string `json:"email"` // recipient
	Token string `json:"token"` // JWT for the action link
}

// PublishEmailEvent dials RabbitMQ, declares the durable queue, and publishes
// a single JSON-encoded EmailEvent. Connections and channels are closed via
// defer so resources are always released, even on error paths.
func PublishEmailEvent(amqpURL string, event EmailEvent) error {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return err
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	// Declare the queue as durable so messages survive a broker restart.
	_, err = ch.QueueDeclare(
		emailQueue,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		return err
	}

	body, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return ch.Publish(
		"",         // default exchange
		emailQueue, // routing key = queue name
		false,      // mandatory
		false,      // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent, // survive broker restart
			Body:         body,
		},
	)
}
