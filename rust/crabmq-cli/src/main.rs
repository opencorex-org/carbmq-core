use std::sync::Arc;

use anyhow::{anyhow, bail, Context, Result};
use clap::{Parser, Subcommand};
use crabmq_parser::{decode_packet, encode_line, Packet, PacketType, Subscription};
use quinn::{ClientConfig, Connection, Endpoint, RecvStream, SendStream};
use rustls::client::danger::{HandshakeSignatureValid, ServerCertVerified, ServerCertVerifier};
use rustls::pki_types::{CertificateDer, ServerName, UnixTime};
use rustls::{DigitallySignedStruct, SignatureScheme};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use uuid::Uuid;

#[derive(Parser)]
#[command(name = "crabmq-rs-cli", about = "Rust QTT client for CrabMQ Core")]
struct Cli {
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    Publish {
        #[arg(long, default_value = "127.0.0.1:1884")]
        addr: String,
        #[arg(long)]
        client_id: String,
        #[arg(long)]
        token: String,
        #[arg(long)]
        topic: String,
        #[arg(long, default_value = "{}")]
        payload: String,
        #[arg(long, default_value_t = 1)]
        qos: u8,
    },
    Subscribe {
        #[arg(long, default_value = "127.0.0.1:1884")]
        addr: String,
        #[arg(long)]
        client_id: String,
        #[arg(long)]
        token: String,
        #[arg(long)]
        topic: String,
    },
}

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();

    match cli.command {
        Command::Publish {
            addr,
            client_id,
            token,
            topic,
            payload,
            qos,
        } => run_publish(&addr, &client_id, &token, &topic, &payload, qos).await,
        Command::Subscribe {
            addr,
            client_id,
            token,
            topic,
        } => run_subscribe(&addr, &client_id, &token, &topic).await,
    }
}

async fn run_publish(
    addr: &str,
    client_id: &str,
    token: &str,
    topic: &str,
    payload: &str,
    qos: u8,
) -> Result<()> {
    let value = serde_json::from_str(payload).context("payload must be valid json")?;
    let (mut send, mut reader) = connect(addr, client_id, token).await?;

    let mut publish = Packet::new(PacketType::Publish);
    publish.packet_id = Some(Uuid::new_v4());
    publish.client_id = Some(client_id.to_string());
    publish.topic = Some(topic.to_string());
    publish.qos = Some(qos);
    publish.payload = Some(value);
    write_packet(&mut send, &publish).await?;

    if qos > 0 {
        let packet = read_packet(&mut reader).await?;
        println!("{}", serde_json::to_string_pretty(&packet)?);
    }

    Ok(())
}

async fn run_subscribe(addr: &str, client_id: &str, token: &str, topic: &str) -> Result<()> {
    let (mut send, mut reader) = connect(addr, client_id, token).await?;

    let mut subscribe = Packet::new(PacketType::Subscribe);
    subscribe.packet_id = Some(Uuid::new_v4());
    subscribe.client_id = Some(client_id.to_string());
    subscribe.subscriptions = Some(vec![Subscription {
        topic: topic.to_string(),
        qos: 1,
    }]);
    write_packet(&mut send, &subscribe).await?;

    let packet = read_packet(&mut reader).await?;
    if !matches!(packet.packet_type, PacketType::SubAck) {
        bail!("expected SUBACK, got {:?}", packet.packet_type);
    }

    loop {
        let packet = read_packet(&mut reader).await?;
        println!("{}", serde_json::to_string_pretty(&packet)?);

        if matches!(packet.packet_type, PacketType::Publish) && packet.qos.unwrap_or_default() > 0 {
            let mut ack = Packet::new(PacketType::PubAck);
            ack.packet_id = packet.packet_id;
            ack.client_id = Some(client_id.to_string());
            write_packet(&mut send, &ack).await?;
        }
    }
}

async fn connect(addr: &str, client_id: &str, token: &str) -> Result<(SendStream, BufReader<RecvStream>)> {
    let bind_addr = "[::]:0".parse().context("invalid bind addr")?;
    let mut endpoint = Endpoint::client(bind_addr)?;
    endpoint.set_default_client_config(insecure_client_config()?);

    let server_name = addr
        .split(':')
        .next()
        .filter(|value| !value.is_empty())
        .unwrap_or("localhost")
        .to_string();
    let connection = endpoint
        .connect(addr.parse().context("invalid remote addr")?, &server_name)?
        .await?;

    let (mut send, recv) = connection.open_bi().await?;
    let mut reader = BufReader::new(recv);

    let mut connect_packet = Packet::new(PacketType::Connect);
    connect_packet.client_id = Some(client_id.to_string());
    connect_packet.token = Some(token.to_string());
    write_packet(&mut send, &connect_packet).await?;

    let response = read_packet(&mut reader).await?;
    if !matches!(response.packet_type, PacketType::ConnAck) {
        bail!("expected CONNACK, got {:?}", response.packet_type);
    }

    Ok((send, reader))
}

async fn write_packet(send: &mut SendStream, packet: &Packet) -> Result<()> {
    let bytes = encode_line(packet)?;
    send.write_all(&bytes).await?;
    send.flush().await?;
    Ok(())
}

async fn read_packet(reader: &mut BufReader<RecvStream>) -> Result<Packet> {
    let mut line = String::new();
    let read = reader.read_line(&mut line).await?;
    if read == 0 {
        return Err(anyhow!("broker closed the stream"));
    }

    decode_packet(line.trim().as_bytes())
}

fn insecure_client_config() -> Result<ClientConfig> {
    let crypto = rustls::ClientConfig::builder()
        .dangerous()
        .with_custom_certificate_verifier(Arc::new(SkipServerVerification))
        .with_no_client_auth();

    Ok(ClientConfig::new(Arc::new(quinn::crypto::rustls::QuicClientConfig::try_from(
        crypto,
    )?)))
}

#[derive(Debug)]
struct SkipServerVerification;

impl ServerCertVerifier for SkipServerVerification {
    fn verify_server_cert(
        &self,
        _end_entity: &CertificateDer<'_>,
        _intermediates: &[CertificateDer<'_>],
        _server_name: &ServerName<'_>,
        _ocsp_response: &[u8],
        _now: UnixTime,
    ) -> Result<ServerCertVerified, rustls::Error> {
        Ok(ServerCertVerified::assertion())
    }

    fn verify_tls12_signature(
        &self,
        _message: &[u8],
        _cert: &CertificateDer<'_>,
        _dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        Ok(HandshakeSignatureValid::assertion())
    }

    fn verify_tls13_signature(
        &self,
        _message: &[u8],
        _cert: &CertificateDer<'_>,
        _dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        Ok(HandshakeSignatureValid::assertion())
    }

    fn supported_verify_schemes(&self) -> Vec<SignatureScheme> {
        vec![
            SignatureScheme::ECDSA_NISTP256_SHA256,
            SignatureScheme::ECDSA_NISTP384_SHA384,
            SignatureScheme::RSA_PSS_SHA256,
            SignatureScheme::RSA_PSS_SHA384,
            SignatureScheme::RSA_PKCS1_SHA256,
        ]
    }
}

#[allow(dead_code)]
fn _connection_addr(connection: &Connection) -> String {
    connection.remote_address().to_string()
}
