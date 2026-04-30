use anyhow::Result;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum PacketType {
    Connect,
    ConnAck,
    Publish,
    PubAck,
    Subscribe,
    SubAck,
    Ping,
    Pong,
    Disconnect,
    Error,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Subscription {
    pub topic: String,
    pub qos: u8,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Packet {
    #[serde(rename = "type")]
    pub packet_type: PacketType,
    #[serde(rename = "packetId", skip_serializing_if = "Option::is_none")]
    pub packet_id: Option<Uuid>,
    #[serde(rename = "clientId", skip_serializing_if = "Option::is_none")]
    pub client_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub token: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub topic: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub qos: Option<u8>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub payload: Option<Value>,
    pub timestamp: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub subscriptions: Option<Vec<Subscription>>,
    #[serde(rename = "sessionPresent", skip_serializing_if = "Option::is_none")]
    pub session_present: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub code: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

impl Packet {
    pub fn new(packet_type: PacketType) -> Self {
        Self {
            packet_type,
            packet_id: None,
            client_id: None,
            token: None,
            topic: None,
            qos: None,
            payload: None,
            timestamp: chrono_timestamp(),
            subscriptions: None,
            session_present: None,
            status: None,
            code: None,
            error: None,
        }
    }
}

pub fn encode_packet(packet: &Packet) -> Result<Vec<u8>> {
    Ok(serde_json::to_vec(packet)?)
}

pub fn encode_line(packet: &Packet) -> Result<Vec<u8>> {
    let mut bytes = encode_packet(packet)?;
    bytes.push(b'\n');
    Ok(bytes)
}

pub fn decode_packet(bytes: &[u8]) -> Result<Packet> {
    Ok(serde_json::from_slice(bytes)?)
}

fn chrono_timestamp() -> String {
    use std::time::{SystemTime, UNIX_EPOCH};

    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default();
    let datetime = time::OffsetDateTime::from_unix_timestamp(now.as_secs() as i64)
        .unwrap_or(time::OffsetDateTime::UNIX_EPOCH)
        + time::Duration::nanoseconds(now.subsec_nanos() as i64);
    datetime
        .format(&time::format_description::well_known::Rfc3339)
        .unwrap_or_else(|_| "1970-01-01T00:00:00Z".to_string())
}
