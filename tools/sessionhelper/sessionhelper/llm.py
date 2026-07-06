"""LLM providers for SH_MODE=llm.

This module deliberately uses the Python standard library HTTP client so the
PyInstaller binary does not depend on provider SDK internals. API keys are read
only from environment-derived Config.
"""

from __future__ import annotations

import json
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass

from .config import Config


@dataclass(frozen=True)
class ProviderDefault:
    base_url: str
    model: str
    kind: str = "openai_compatible"


PROVIDERS: dict[str, ProviderDefault] = {
    "deepseek": ProviderDefault("https://api.deepseek.com/v1", "deepseek-chat"),
    "qwen": ProviderDefault("https://dashscope.aliyuncs.com/compatible-mode/v1", "qwen-plus"),
    "kimi": ProviderDefault("https://api.moonshot.cn/v1", "moonshot-v1-8k"),
    "minimax": ProviderDefault("https://api.minimax.chat/v1", "MiniMax-Text-01"),
    "glm": ProviderDefault("https://open.bigmodel.cn/api/paas/v4", "glm-4-flash"),
    "openai": ProviderDefault("https://api.openai.com/v1", "gpt-4o-mini"),
    "claude": ProviderDefault("https://api.anthropic.com", "claude-3-5-sonnet-latest", "anthropic"),
    "gemini": ProviderDefault("https://generativelanguage.googleapis.com/v1beta", "gemini-1.5-flash", "google"),
}


class LLMProvider:
    def __init__(self, cfg: Config):
        if cfg.provider not in PROVIDERS:
            raise RuntimeError(f"unsupported SH_PROVIDER={cfg.provider}")
        if not cfg.api_key:
            raise RuntimeError("SH_API_KEY is required for SH_MODE=llm")
        default = PROVIDERS[cfg.provider]
        self.provider = cfg.provider
        self.kind = default.kind
        self.base_url = (cfg.base_url or default.base_url).rstrip("/")
        self.model = cfg.model or default.model
        self.api_key = cfg.api_key
        self.system_prompt = cfg.system_prompt
        self.history_turns = max(1, cfg.history_turns)
        self.history: dict[str, list[dict[str, str]]] = {}

    def complete(self, conversation_id: str, user_text: str) -> str:
        history = self.history.setdefault(conversation_id, [])
        history.append({"role": "user", "content": user_text})
        history[:] = history[-self.history_turns * 2 :]
        if self.kind == "anthropic":
            reply = self._anthropic(history)
        elif self.kind == "google":
            reply = self._google(history)
        else:
            reply = self._openai_compatible(history)
        history.append({"role": "assistant", "content": reply})
        history[:] = history[-self.history_turns * 2 :]
        return reply

    def _openai_compatible(self, history: list[dict[str, str]]) -> str:
        payload = {
            "model": self.model,
            "messages": [{"role": "system", "content": self.system_prompt}] + history,
            "temperature": 0.3,
        }
        data = self._post_json(
            f"{self.base_url}/chat/completions",
            payload,
            {"Authorization": f"Bearer {self.api_key}"},
        )
        return str(data["choices"][0]["message"]["content"]).strip()

    def _anthropic(self, history: list[dict[str, str]]) -> str:
        payload = {
            "model": self.model,
            "max_tokens": 2048,
            "system": self.system_prompt,
            "messages": history,
        }
        data = self._post_json(
            f"{self.base_url}/v1/messages",
            payload,
            {
                "x-api-key": self.api_key,
                "anthropic-version": "2023-06-01",
            },
        )
        parts = []
        for item in data.get("content", []):
            if item.get("type") == "text" and item.get("text"):
                parts.append(item["text"])
        return "\n".join(parts).strip()

    def _google(self, history: list[dict[str, str]]) -> str:
        contents = []
        for msg in history:
            role = "model" if msg["role"] == "assistant" else "user"
            contents.append({"role": role, "parts": [{"text": msg["content"]}]})
        payload = {
            "systemInstruction": {"parts": [{"text": self.system_prompt}]},
            "contents": contents,
        }
        query = urllib.parse.urlencode({"key": self.api_key})
        data = self._post_json(
            f"{self.base_url}/models/{self.model}:generateContent?{query}",
            payload,
            {},
        )
        parts = []
        for cand in data.get("candidates", []):
            for part in cand.get("content", {}).get("parts", []):
                if part.get("text"):
                    parts.append(part["text"])
        return "\n".join(parts).strip()

    @staticmethod
    def _post_json(url: str, payload: dict, headers: dict[str, str]) -> dict:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        req = urllib.request.Request(
            url,
            data=body,
            method="POST",
            headers={
                "Content-Type": "application/json",
                **headers,
            },
        )
        try:
            with urllib.request.urlopen(req, timeout=60) as resp:
                return json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"LLM upstream HTTP {exc.code}: {detail[:500]}") from exc
