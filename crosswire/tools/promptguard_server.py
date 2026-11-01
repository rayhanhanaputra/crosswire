#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["transformers", "torch"]
# ///
"""Serve Llama Prompt Guard 2 (86M) as a Crosswire guardrail classifier.

Interface, for `crosswire target NAME --guardrail=... --classifier-url URL`:

  POST /  {"text": "..."}  ->  {"block": bool, "score": float}

score is the probability of the malicious class. block is score >= --threshold.
Text longer than the model's 512-token window is scored in overlapping windows
and the highest score wins.

The weights (meta-llama/Llama-Prompt-Guard-2-86M) are gated by the Llama licence.
Accept it on huggingface.co with your account, then give this process a token in
HF_TOKEN or HUGGINGFACE_HUB_TOKEN, or point --model at a local copy. The script
never downloads without one of those and exits with a message instead.

Usage:
  uv run promptguard_server.py --port 7301
  uv run promptguard_server.py --model /path/to/local/Llama-Prompt-Guard-2-86M --threshold 0.5
"""
import argparse
import json
import os
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Callable

MODEL_ID = "meta-llama/Llama-Prompt-Guard-2-86M"
WINDOW = 512
STRIDE = 256
MAX_BODY = 4 << 20


def load_scorer(model: str) -> Callable[[str], float]:
    """Return a function text -> P(malicious). Raises SystemExit when the weights are unavailable."""
    local = os.path.isdir(model)
    token = os.environ.get("HF_TOKEN") or os.environ.get("HUGGINGFACE_HUB_TOKEN")
    if not local and not token:
        raise SystemExit(
            f"{model} is gated and no token is set. Set HF_TOKEN or HUGGINGFACE_HUB_TOKEN after accepting "
            "the licence, or pass --model with a local directory."
        )
    try:
        import torch
        from transformers import AutoModelForSequenceClassification, AutoTokenizer
    except ImportError as exc:
        raise SystemExit(f"{exc}. Run with `uv run`, or install transformers and torch.")
    kwargs = {} if local else {"token": token}
    tok = AutoTokenizer.from_pretrained(model, **kwargs)
    net = AutoModelForSequenceClassification.from_pretrained(model, **kwargs).eval()
    labels = {int(i): str(n).lower() for i, n in net.config.id2label.items()}
    bad = next((i for i, n in labels.items() if "malicious" in n or "injection" in n or "jailbreak" in n), 1)

    def score(text: str) -> float:
        ids = tok(text, add_special_tokens=False, return_attention_mask=False)["input_ids"]
        room = WINDOW - tok.num_special_tokens_to_add(pair=False)
        starts = range(0, max(len(ids) - room, 0) + 1, STRIDE) if len(ids) > room else [0]
        best = 0.0
        for s in starts:
            chunk = tok.build_inputs_with_special_tokens(ids[s : s + room])
            batch = torch.tensor([chunk])
            with torch.no_grad():
                logits = net(input_ids=batch, attention_mask=torch.ones_like(batch)).logits
            best = max(best, float(torch.softmax(logits, dim=-1)[0, bad]))
        return best

    return score


def make_handler(score: Callable[[str], float], threshold: float) -> type[BaseHTTPRequestHandler]:
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args: object) -> None:
            pass

        def _send(self, code: int, body: dict) -> None:
            raw = json.dumps(body).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def do_POST(self) -> None:
            try:
                size = int(self.headers.get("Content-Length", "0"))
                if not 0 < size <= MAX_BODY:
                    return self._send(400, {"error": "body missing or too large"})
                text = json.loads(self.rfile.read(size)).get("text")
                if not isinstance(text, str):
                    return self._send(400, {"error": 'want {"text": string}'})
                s = score(text)
            except (ValueError, KeyError) as exc:
                return self._send(400, {"error": str(exc)})
            self._send(200, {"block": s >= threshold, "score": round(s, 6)})

        def do_GET(self) -> None:
            self._send(200, {"model": MODEL_ID, "threshold": threshold})

    return Handler


def main() -> int:
    ap = argparse.ArgumentParser(description="Prompt Guard 2 behind Crosswire's classifier interface")
    ap.add_argument("--model", default=MODEL_ID, help="Hugging Face id or a local directory")
    ap.add_argument("--port", type=int, default=7301)
    ap.add_argument("--threshold", type=float, default=0.5, help="block when P(malicious) is at least this")
    args = ap.parse_args()
    score = load_scorer(args.model)
    server = ThreadingHTTPServer(("127.0.0.1", args.port), make_handler(score, args.threshold))
    print(f"prompt guard on http://127.0.0.1:{args.port}/ threshold {args.threshold}", file=sys.stderr)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    return 0


if __name__ == "__main__":
    sys.exit(main())
