"""
Cloud fine-tune of the Sahayak intent router — ONE command, on a Modal GPU.

    train QLoRA  ->  merge 16-bit  ->  convert to q8_0 GGUF  ->  save to a volume

Adapted from the proven research pipeline (researchwork/finetuning/tryone), with two
deliberate changes for THIS project:

  1. BASE MODEL is Apache-2.0 (Qwen3-4B-Instruct), NOT Gemma. Per the project's
     model-licensing rule, only Apache-2.0/MIT weights may ship in the embedded
     appliance; Gemma is excluded. (Gemma is fine for a DEV-ONLY model run via Ollama,
     but this pipeline targets the shippable router.)  >>> VERIFY the exact unsloth tag
     and its license before running (see MODEL_NAME note).

  2. GENTLE recipe by default (r=8, lr=5e-5, 1 epoch). Your prior aggressive run
     (r=16, lr=2e-4, 2ep) OVERFIT and scored BELOW the base model. Small nudge only.

ONE-TIME SETUP (on your machine — needs YOUR Modal account; a security boundary):
    pip install modal
    modal token new

BEFORE TRAINING (offline, no GPU):
    python gen_dataset.py            # writes router_dataset_train.jsonl (+ _eval)

EVERY RETRAIN:
    modal run finetune_modal.py
    modal volume get sahayak-router-out sahayak-router-q8_0.gguf .

THEN (import into Ollama — the CLI's backend — and PROVE it beats the base):
    ollama create sahayak-router -f Modelfile
    python eval.py --model qwen3:4b-instruct --data router_dataset_eval.jsonl   # base
    python eval.py --model sahayak-router    --data router_dataset_eval.jsonl   # tuned
    # Ship ONLY if the fine-tune's FULL accuracy clearly beats the base's.
"""

import os
import modal

DATA_FILE = "router_dataset_train.jsonl"
DATA_LOCAL = os.path.join(os.path.dirname(os.path.abspath(__file__)), DATA_FILE)

# >>> VERIFY THIS TAG + LICENSE before running. Qwen3-4B-Instruct is Apache-2.0; pick the
# matching unsloth 4-bit build. Known-good Apache-2.0 alternatives if the tag drifts:
#   unsloth/Qwen2.5-3B-Instruct-unsloth-bnb-4bit
#   unsloth/Qwen2.5-7B-Instruct-unsloth-bnb-4bit  (bigger; A10G still fine)
MODEL_NAME = "unsloth/Qwen3-4B-Instruct-2507-unsloth-bnb-4bit"
MAX_SEQ_LEN = 1024            # the classify prompt is short; this is ample headroom

# GENTLE recipe — the anti-forgetting lesson from the prior router fine-tune.
EPOCHS = 1
LORA_R = 8
LORA_ALPHA = 16              # alpha/r = 2.0
LEARNING_RATE = 5e-5
GGUF_OUT = "sahayak-router-q8_0.gguf"

image = (
    modal.Image.debian_slim(python_version="3.11")
    .apt_install("git")
    .pip_install(
        "unsloth",
        "datasets>=3.4.1,<4.4.0",
        "trl>=0.18.2,<=0.24.0",
        "bitsandbytes",
        "sentencepiece",
        "protobuf",
        "gguf",
    )
    .run_commands("git clone --depth 1 https://github.com/ggerganov/llama.cpp /llama.cpp")
    .add_local_file(DATA_LOCAL, f"/root/{DATA_FILE}")
)

app = modal.App("sahayak-router-finetune")
out_vol = modal.Volume.from_name("sahayak-router-out", create_if_missing=True)


@app.function(image=image, gpu="A10G", timeout=60 * 60, volumes={"/out": out_vol})
def train() -> str:
    import subprocess
    from unsloth import FastLanguageModel
    from datasets import load_dataset
    from trl import SFTTrainer, SFTConfig

    print(f"[STAGE] loading {MODEL_NAME}", flush=True)
    model, tokenizer = FastLanguageModel.from_pretrained(
        model_name=MODEL_NAME,
        max_seq_length=MAX_SEQ_LEN,
        load_in_4bit=True,
        dtype=None,
    )
    model = FastLanguageModel.get_peft_model(
        model,
        r=LORA_R,
        lora_alpha=LORA_ALPHA,
        lora_dropout=0.0,
        target_modules=["q_proj", "k_proj", "v_proj", "o_proj",
                        "gate_proj", "up_proj", "down_proj"],
        use_gradient_checkpointing="unsloth",
        random_state=42,
    )

    print("[STAGE] formatting dataset", flush=True)
    ds = load_dataset("json", data_files=f"/root/{DATA_FILE}", split="train")

    def fmt(row):
        return {"text": tokenizer.apply_chat_template(
            row["messages"], tokenize=False, add_generation_prompt=False)}

    ds = ds.map(fmt)
    print(f"[STAGE] {len(ds)} examples", flush=True)

    trainer = SFTTrainer(
        model=model,
        processing_class=tokenizer,
        train_dataset=ds,
        args=SFTConfig(
            dataset_text_field="text",
            max_seq_length=MAX_SEQ_LEN,
            per_device_train_batch_size=2,
            gradient_accumulation_steps=4,
            warmup_steps=5,
            num_train_epochs=EPOCHS,
            learning_rate=LEARNING_RATE,
            logging_steps=1,
            optim="adamw_8bit",
            weight_decay=0.01,
            lr_scheduler_type="linear",
            seed=42,
            bf16=True,
            output_dir="/out/checkpoints",
            report_to="none",
        ),
    )

    print("[STAGE] training START", flush=True)
    trainer.train()
    print("[STAGE] training DONE -> merging 16-bit", flush=True)
    model.save_pretrained_merged("/out/merged", tokenizer, save_method="merged_16bit")

    print("[STAGE] converting merged -> q8_0 GGUF", flush=True)
    subprocess.run(
        ["python", "/llama.cpp/convert_hf_to_gguf.py", "/out/merged",
         "--outfile", f"/out/{GGUF_OUT}", "--outtype", "q8_0"],
        check=True,
    )
    out_vol.commit()
    print(f"[STAGE] ALL DONE -> /out/{GGUF_OUT}", flush=True)
    return GGUF_OUT


@app.local_entrypoint()
def main():
    name = train.remote()
    print(f"\nDone. Download it with:\n  modal volume get sahayak-router-out {name} .\n"
          f"Then: ollama create sahayak-router -f Modelfile  &&  python eval.py ...\n")
