# Rtpengine-Mon

Rtpengine-Mon is a simple tool for monitoring active calls and statistics from **RTPEngine**.

## Features

- **Active Call Monitoring**: View all current calls handled by RTPEngine in real-time.
- **Statistics Dashboard**: View RTPEngine statistics.
- **Audio Spying**: High-quality audio monitoring of calls using WebRTC.

## Screenshots

![Dashboard](screenshots/2026-02-07_23-09.png)

![Active Calls](screenshots/2026-02-07_23-55.png)

![Call Details & Spying](screenshots/2026-02-07_23-08.png)


## Getting Started

### Prerequisites

- Go 1.21 or later
- RTPEngine instance

### Configuration

Copy the `.env.example` file to `.env` and configure the necessary variables:

```bash
cp .env.example .env
```

Key configuration options:
- `HTTP_PORT`: Port for the web interface (default: 8081).
- `RTPENGINE_ADDR`: Address of the RTPEngine Control channel.
- `WEBRTC_NAT_1TO1_IPS`: comma separated list of IPs for WebRTC NAT 1to1 mapping.
- `WEBRTC_ICE_ADDRESS`: Public/Local IP address for WebRTC ICE candidates.

### Running the Application

#### Using Go
```bash
go run cmd/rtpengine-mon/main.go
```
