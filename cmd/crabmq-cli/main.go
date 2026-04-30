package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	qttclient "github.com/opencorex/crabmq-core/pkg/qtt/client"
	qtttypes "github.com/opencorex/crabmq-core/pkg/qtt/types"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "publish":
		if err := runPublish(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "subscribe":
		if err := runSubscribe(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func runPublish(args []string) error {
	flags := flag.NewFlagSet("publish", flag.ContinueOnError)
	addr := flags.String("addr", env("CRABMQ_BROKER_ADDR", "127.0.0.1:1884"), "broker address")
	clientID := flags.String("client", env("CRABMQ_CLIENT_ID", "cli-publisher"), "client id")
	token := flags.String("token", os.Getenv("CRABMQ_TOKEN"), "jwt token")
	topic := flags.String("topic", "", "topic name")
	payload := flags.String("payload", "{}", "json payload")
	qos := flags.Int("qos", 1, "qos level")
	if err := flags.Parse(args); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	client, err := qttclient.Dial(ctx, qttclient.Config{
		Addr:     *addr,
		ClientID: *clientID,
		Token:    *token,
	})
	if err != nil {
		return err
	}
	defer client.Close()

	if _, err := client.Connect(ctx, map[string]string{"source": "go-cli"}); err != nil {
		return err
	}

	if !json.Valid([]byte(*payload)) {
		return fmt.Errorf("payload must be valid json")
	}

	if err := client.Publish(ctx, *topic, *qos, []byte(*payload)); err != nil {
		return err
	}

	if *qos > 0 {
		packet, err := client.ReadPacket(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("received %s for packet %s\n", packet.Type, packet.PacketID)
	}

	return nil
}

func runSubscribe(args []string) error {
	flags := flag.NewFlagSet("subscribe", flag.ContinueOnError)
	addr := flags.String("addr", env("CRABMQ_BROKER_ADDR", "127.0.0.1:1884"), "broker address")
	clientID := flags.String("client", env("CRABMQ_CLIENT_ID", "cli-subscriber"), "client id")
	token := flags.String("token", os.Getenv("CRABMQ_TOKEN"), "jwt token")
	topic := flags.String("topic", "", "topic filter")
	if err := flags.Parse(args); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	client, err := qttclient.Dial(ctx, qttclient.Config{
		Addr:     *addr,
		ClientID: *clientID,
		Token:    *token,
	})
	if err != nil {
		return err
	}
	defer client.Close()

	if _, err := client.Connect(ctx, map[string]string{"source": "go-cli"}); err != nil {
		return err
	}

	if err := client.Subscribe(ctx, []qtttypes.Subscription{{Topic: *topic, QoS: 1}}); err != nil {
		return err
	}

	if _, err := client.ReadPacket(ctx); err != nil {
		return err
	}

	for {
		packet, err := client.ReadPacket(ctx)
		if err != nil {
			return err
		}

		pretty, _ := json.MarshalIndent(packet, "", "  ")
		fmt.Println(string(pretty))
		if packet.Type == qtttypes.PacketPublish && packet.QoS > 0 {
			ackCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = client.SendPubAck(ackCtx, packet.PacketID)
			cancel()
		}
	}
}

func usage() {
	fmt.Println("usage:")
	fmt.Println("  crabmq-cli publish -addr 127.0.0.1:1884 -client api-tool -token <jwt> -topic device/demo/command -payload '{\"action\":\"reboot\"}'")
	fmt.Println("  crabmq-cli subscribe -addr 127.0.0.1:1884 -client api-tool -token <jwt> -topic device/+/telemetry")
}

func env(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
