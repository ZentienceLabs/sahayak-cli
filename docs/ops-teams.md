---
layout: default
title: Ops Teams Guide
nav_order: 6
---

**Navigation:** [Home](./README.md) | [Installation](./installation.md) | [Configuration](./configuration.md) | [Commands](./commands.md) | [Cartridges](./cartridges.md) | [Ops Teams](./ops-teams.md) | [Self-Learning](./self-learning.md) | [Embedded Appliance](./embedded-appliance.md)

***

# Sahayak for Ops Teams

Sahayak is not just a personal AI chatbot—it is designed to be a **Team Playbook Engine**. For mature Ops and Platform Engineering teams, Sahayak solves the friction of discoverability, onboarding, and safely executing standard operating procedures (SOPs).

## The Problem with Traditional Scripts
Most teams have a Git repository with hundreds of bash and Python scripts (`restart-web.sh`, `drain-node.py`, etc.). 
* **Discoverability:** Junior engineers don't know the exact script names.
* **Execution Error:** Remembering the exact order of positional arguments (`./scale.sh us-east-1 web 5`) is error-prone.
* **Safety:** Legacy scripts often lack confirmation prompts and execute mutating changes instantly.

## The Sahayak Solution
Sahayak doesn't replace your scripts; it **wraps** them with a safe, natural-language interface.
1. A senior engineer authors a **Cartridge** (a JSON template) mapping to the script.
2. An on-call engineer types: `sahayak ask "scale the web app to 5 instances in us-east-1"`
3. Sahayak's SLM extracts the arguments (`app: web`, `count: 5`, `region: us-east-1`).
4. Sahayak drafts the command and **pauses at the approval gate** (`[a]pprove / [e]dit / [r]eject`).

## Recommended Deployment Architectures

You don't want 50 engineers downloading 4GB models to their laptops. Use one of these architectures instead:

### 1. The "Hub and Spoke" (Team Server)
* **The Hub:** Provision one internal server on your VPN running Ollama (`ollama serve`) and the `qwen3.5:4b` model.
* **The Spokes:** Engineers download ONLY the 20MB `sahayak.exe` binary to their laptops. 
* **Config:** They set `$env:SAHAYAK_ENDPOINT="http://internal-ai-server:11434"`.
* **Benefit:** Zero laptop battery drain, instant setup, and 100% data sovereignty (data never leaves the VPN).

### 2. The Jump Box / Bastion Host (Zero-Install)
* Install Sahayak and Ollama directly on your team's secure Jump Box (Bastion Host).
* **Config:** Put the team's Cartridges in a shared folder (e.g., `/opt/sahayak/cartridges`) and set the env vars in `/etc/profile.d/sahayak.sh`.
* **Benefit:** Engineers SSH into the jump box and use Sahayak instantly. No local installation required. It automatically inherits the strict IAM and network boundaries of the jump box, making it highly secure and fully air-gapped.

## Cloud Fallback vs. 100% Sovereign Hub for Complex Diagnosis

Sahayak handles 95% of routine tasks locally on the engineer's laptop using the lightweight 4B model. However, when an engineer asks a complex diagnostic question (e.g., *"Why is the payment pod crashing?"*), a 4B model cannot reason through complex logs. You have two choices for this:

### Option A: The Opt-In Cloud Fallback (Claude 4 / GPT-4)
Sahayak can be configured to use a cloud API for heavy reasoning. 
* When invoked, Sahayak uses `core/redact` to strip IPs, secrets, and internal domains.
* It presents the **masked payload** to the user for explicit approval before sending it to the cloud.
* **Benefit:** You get frontier-level debugging with zero hardware costs, while maintaining strict data privacy.

### Option B: The 100% Sovereign Air-Gapped Hub (No Cloud)
If your company is highly regulated and refuses any cloud API, you can run a frontier-level model entirely on your internal network. 

**The Ideal Models (27B - 32B Class)**
Models like **Qwen 3.8 27B** or **DeepSeek-R1-Distill 32B** are the sweet spot. They are smart enough to perform complex diagnostic reasoning (replacing Claude), but small enough to run on a single enterprise workstation.

**Recommended Hub Hardware:**
You do not need a $40,000 data center GPU. You can run this setup on:
1. **Mac Studio (Apple Silicon):** An M2 Ultra or M4 Max with 64GB of Unified Memory can run a 32B or 70B model blazingly fast because the GPU shares the system RAM.
2. **Linux Workstation:** A server with 2x NVIDIA RTX 3090/4090 GPUs (48GB total VRAM) allows a 32B model to sit entirely in video memory for instant response times.

**Result:** Your engineers get GPT-4 level debugging assistance, but your production logs and infrastructure data literally never leave your corporate VPN.
