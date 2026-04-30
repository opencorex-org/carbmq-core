package quic

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"sync"
	"time"

	quicgo "github.com/quic-go/quic-go"

	"github.com/opencorex/crabmq-core/internal/config"
	"github.com/opencorex/crabmq-core/internal/protocol/decoder"
	"github.com/opencorex/crabmq-core/internal/protocol/encoder"
	"github.com/opencorex/crabmq-core/internal/transport/connection"
	qtttypes "github.com/opencorex/crabmq-core/pkg/qtt/types"
)

type Server struct {
	cfg    config.BrokerConfig
	logger *slog.Logger
}

func NewServer(cfg config.BrokerConfig, logger *slog.Logger) *Server {
	return &Server{
		cfg:    cfg,
		logger: logger,
	}
}

func (s *Server) Listen(ctx context.Context, handler connection.Handler) error {
	tlsConfig, err := s.tlsConfig()
	if err != nil {
		return err
	}

	listener, err := quicgo.ListenAddr(
		s.cfg.ListenAddr,
		tlsConfig,
		&quicgo.Config{
			KeepAlivePeriod: 20 * time.Second,
			MaxIdleTimeout:  s.cfg.ReadTimeout,
			Allow0RTT:       false,
			EnableDatagrams: false,
		},
	)
	if err != nil {
		return fmt.Errorf("listen quic: %w", err)
	}
	defer listener.Close()

	s.logger.Info("broker listening", "addr", s.cfg.ListenAddr, "alpn", s.cfg.ProtocolName)

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept quic connection: %w", err)
		}

		go s.handleConn(ctx, conn, handler)
	}
}

func (s *Server) handleConn(ctx context.Context, conn quicgo.Connection, handler connection.Handler) {
	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		s.logger.Warn("failed to accept stream", "remote", conn.RemoteAddr().String(), "error", err)
		_ = conn.CloseWithError(1, "stream error")
		return
	}

	packetConn := &packetConn{
		id:           conn.RemoteAddr().String(),
		conn:         conn,
		stream:       stream,
		encoder:      encoder.NewJSONEncoder(stream),
		decoder:      decoder.NewJSONDecoder(stream),
		writeTimeout: s.cfg.WriteTimeout,
		readTimeout:  s.cfg.ReadTimeout,
	}

	handler(ctx, packetConn)
}

func (s *Server) tlsConfig() (*tls.Config, error) {
	if s.cfg.CertFile != "" && s.cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(s.cfg.CertFile, s.cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load tls keypair: %w", err)
		}
		return &tls.Config{
			NextProtos:   []string{s.cfg.ProtocolName},
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS13,
		}, nil
	}

	cert, err := generateSelfSignedCertificate()
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		NextProtos:   []string{s.cfg.ProtocolName},
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}, nil
}

type packetConn struct {
	id           string
	conn         quicgo.Connection
	stream       quicgo.Stream
	encoder      *encoder.JSONEncoder
	decoder      *decoder.JSONDecoder
	writeTimeout time.Duration
	readTimeout  time.Duration
	mu           sync.Mutex
}

func (c *packetConn) ID() string {
	return c.id
}

func (c *packetConn) RemoteAddr() string {
	return c.conn.RemoteAddr().String()
}

func (c *packetConn) Send(_ context.Context, packet qtttypes.Packet) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.writeTimeout > 0 {
		_ = c.stream.SetWriteDeadline(time.Now().Add(c.writeTimeout))
		defer c.stream.SetWriteDeadline(time.Time{})
	}

	return c.encoder.Encode(packet)
}

func (c *packetConn) Receive(_ context.Context) (qtttypes.Packet, error) {
	if c.readTimeout > 0 {
		_ = c.stream.SetReadDeadline(time.Now().Add(c.readTimeout))
		defer c.stream.SetReadDeadline(time.Time{})
	}

	return c.decoder.Decode()
}

func (c *packetConn) Close() error {
	_ = c.stream.Close()
	return c.conn.CloseWithError(0, "closed")
}

func generateSelfSignedCertificate() (tls.Certificate, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate private key: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parse keypair: %w", err)
	}

	return certificate, nil
}
