import json
import re
import sys

EVAL_ID = 'create-variable'
EVAL_NAME = 'create variable pattern'


def extract_code_blocks(text):
    return re.findall(r'```[\w]*\n([\s\S]*?)```', text)


def main():
    ctx = json.load(sys.stdin)

    if not ctx.get('invoked') or not ctx.get('qualityTranscript'):
        result = {'evalId': EVAL_ID, 'name': EVAL_NAME, 'score': 0.0, 'reasoning': 'Skipped — skill not invoked.'}
        sys.stdout.write(json.dumps(result))
        return

    code = '\n'.join(extract_code_blocks(ctx['qualityTranscript']))

    if code:
        has_global = bool(re.search(r'variable\s+"create"\s*\{', code)) or bool(re.search(r'var\.create\b', code))
        has_per_resource = bool(re.search(r'variable\s+"create_\w+"\s*\{', code)) or bool(re.search(r'var\.create_\w+', code))

        if has_global and has_per_resource:
            result = {'evalId': EVAL_ID, 'name': EVAL_NAME, 'score': 1.0, 'reasoning': 'Code uses both global `create` and per-resource `create_<name>` variables.'}
        elif has_global:
            result = {'evalId': EVAL_ID, 'name': EVAL_NAME, 'score': 0.6, 'reasoning': 'Code uses global `var.create` but missing per-resource `create_<name>` variables.'}
        else:
            result = {'evalId': EVAL_ID, 'name': EVAL_NAME, 'score': 0.5, 'reasoning': 'Code present but no create variable pattern detected — may not be applicable to this scenario.'}
    else:
        result = {'evalId': EVAL_ID, 'name': EVAL_NAME, 'score': 0.5, 'reasoning': 'No code blocks found — cannot determine pattern usage.'}

    sys.stdout.write(json.dumps(result))


if __name__ == '__main__':
    main()
