import requests
import os

env = {}
with open('.env') as f:
    for l in f:
        if '=' in l and not l.startswith('#'):
            k, v = l.strip().split('=', 1)
            env[k] = v

def check_models(url, headers=None, extract_key='data', id_key='id'):
    try:
        res = requests.get(url, headers=headers)
        if res.status_code == 200:
            data = res.json()
            items = data.get(extract_key, [])
            return [m.get(id_key) for m in items if isinstance(m, dict)]
        return None
    except Exception as e:
        return None

models_openai = set(check_models("https://api.openai.com/v1/models", {"Authorization": f"Bearer {env.get('OPENAI_API_KEY')}"}) or [])
models_anthropic = set(check_models("https://api.anthropic.com/v1/models", {"x-api-key": env.get('ANTHROPIC_API_KEY'), "anthropic-version": "2023-06-01"}) or [])
models_google = set(check_models(f"https://generativelanguage.googleapis.com/v1beta/models?key={env.get('GOOGLE_API_KEY')}", extract_key='models', id_key='name') or [])
# Google models have 'models/' prefix
models_google = {m.replace('models/', '') for m in models_google}

models_moonshot = set(check_models("https://api.moonshot.cn/v1/models", {"Authorization": f"Bearer {env.get('MOONSHOT_API_KEY')}"}) or [])
models_qwen = set(check_models("https://dashscope.aliyuncs.com/compatible-mode/v1/models", {"Authorization": f"Bearer {env.get('QWEN_API_KEY', '')}"}) or [])

print(f"OpenAI: {len(models_openai)}, Anthropic: {len(models_anthropic)}, Google: {len(models_google)}, Moonshot: {len(models_moonshot)}, Qwen: {len(models_qwen)}")
