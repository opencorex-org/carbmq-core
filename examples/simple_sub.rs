use anyhow::Result;
use crabmq_parser::{Packet, PacketType, Subscription};
use uuid::Uuid;

fn main() -> Result<()> {
    let packet = Packet {
        packet_type: PacketType::Subscribe,
        packet_id: Some(Uuid::new_v4()),
        client_id: Some("example-cli".to_string()),
        token: None,
        topic: None,
        qos: None,
        payload: None,
        timestamp: "2026-04-30T10:15:30Z".to_string(),
        subscriptions: Some(vec![Subscription {
            topic: "device/+/telemetry".to_string(),
            qos: 1,
        }]),
        session_present: None,
        status: None,
        code: None,
        error: None,
    };

    println!("{}", serde_json::to_string_pretty(&packet)?);
    Ok(())
}
