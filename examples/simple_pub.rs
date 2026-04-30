use anyhow::Result;
use crabmq_parser::{encode_packet, Packet, PacketType};
use serde_json::json;
use uuid::Uuid;

fn main() -> Result<()> {
    let packet = Packet {
        packet_type: PacketType::Publish,
        packet_id: Some(Uuid::new_v4()),
        client_id: Some("example-cli".to_string()),
        token: None,
        topic: Some("device/demo-device/telemetry".to_string()),
        qos: Some(1),
        payload: Some(json!({
            "temperature": 21.7,
            "humidity": 44.2
        })),
        timestamp: "2026-04-30T10:15:30Z".to_string(),
        subscriptions: None,
        session_present: None,
        status: None,
        code: None,
        error: None,
    };

    let encoded = encode_packet(&packet)?;
    println!("encoded {} bytes", encoded.len());
    Ok(())
}
