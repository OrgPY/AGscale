import json
import os
from typing import Any

import httpx
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

app = FastAPI(title="agscale-demo-agent", version="0.1.0")


class ChatRequest(BaseModel):
    message: str


def _parse_json_env(name: str) -> dict[str, Any]:
    raw = os.getenv(name, "")
    if not raw:
        return {}
    try:
        parsed = json.loads(raw)
        if isinstance(parsed, dict):
            return parsed
    except json.JSONDecodeError:
        pass
    return {}


def _resolve_tool_endpoint() -> str:
    endpoints = _parse_json_env("AGSCALE_SKILL_ENDPOINTS_JSON")
    refs = os.getenv("AGSCALE_SKILL_REFS", "")
    if refs:
        for name in [r.strip() for r in refs.split(",") if r.strip()]:
            endpoint = endpoints.get(name)
            if endpoint:
                return str(endpoint)
    for _, endpoint in endpoints.items():
        return str(endpoint)
    return ""


async def _call_local_tool() -> dict[str, Any]:
    endpoint = _resolve_tool_endpoint().rstrip("/")
    if not endpoint:
        return {"called": False, "reason": "no tool endpoint configured"}

    url = f"{endpoint}/get"
    try:
        async with httpx.AsyncClient(timeout=5.0) as client:
            resp = await client.get(url)
            resp.raise_for_status()
            payload = resp.json() if resp.headers.get("content-type", "").startswith("application/json") else {"body": resp.text}
            return {
                "called": True,
                "url": url,
                "status": resp.status_code,
                "keys": sorted(list(payload.keys())) if isinstance(payload, dict) else [],
            }
    except Exception as exc:
        return {"called": False, "url": url, "error": str(exc)}


async def _chat_openai(message: str) -> dict[str, Any]:
    provider = os.getenv("AGSCALE_LLM_PROVIDER", "openclaw")
    api_key = os.getenv("OPENAI_API_KEY", "") or os.getenv("AGSCALE_LLM_API_KEY", "")
    if not api_key:
        raise HTTPException(status_code=503, detail="OPENAI_API_KEY or AGSCALE_LLM_API_KEY is required in openai mode")

    model = os.getenv("OPENAI_MODEL", os.getenv("AGSCALE_LLM_MODEL", "gpt-4o-mini"))
    base_url = os.getenv("AGSCALE_LLM_ENDPOINT", "").rstrip("/")
    if not base_url:
        base_url = os.getenv("OPENAI_API_BASE", "https://api.openai.com/v1").rstrip("/")
    body = {
        "model": model,
        "messages": [
            {"role": "system", "content": "You are a concise assistant running inside AGScale demo."},
            {"role": "user", "content": message},
        ],
        "temperature": 0.2,
    }

    headers = {"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"}
    async with httpx.AsyncClient(timeout=25.0) as client:
        resp = await client.post(f"{base_url}/chat/completions", headers=headers, json=body)
        resp.raise_for_status()
        data = resp.json()

    try:
        content = data["choices"][0]["message"]["content"]
    except Exception as exc:
        raise HTTPException(status_code=502, detail=f"unexpected OpenAI response shape: {exc}")

    return {
        "mode": "openai-compatible",
        "provider": provider,
        "endpoint": base_url,
        "model": model,
        "reply": content,
        "usage": data.get("usage", {}),
    }


@app.get("/")
async def root() -> dict[str, str]:
    return {"service": "agscale-demo-agent", "status": "ok"}


@app.get("/healthz")
async def healthz() -> dict[str, str]:
    return {"status": "ok"}


@app.get("/livez")
async def livez() -> dict[str, str]:
    return {"status": "ok"}


@app.post("/chat")
async def chat(req: ChatRequest) -> dict[str, Any]:
    mode = os.getenv("AGENT_MODE", "local").strip().lower()
    agent_name = os.getenv("AGSCALE_AGENT_NAME", "unknown-agent")

    if mode in ("openai", "openclaw", "opencode"):
        result = await _chat_openai(req.message)
        result["agent"] = agent_name
        return result

    tool = await _call_local_tool()
    return {
        "mode": "local",
        "agent": agent_name,
        "reply": f"[{agent_name}] local response to: {req.message}",
        "tool": tool,
    }
