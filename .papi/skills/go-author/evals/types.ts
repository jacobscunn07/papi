export interface Scenario {
  id: string;
  prompt: string;
  fixtures?: Record<string, string>;
  tags?: string[];
  shouldInvoke?: boolean;
}

/** The JSON `claude --output-format json` returns for a phase. */
export interface ClaudeJsonOutput {
  type: string;
  subtype: string;
  is_error: boolean;
  result: string;
  session_id: string;
  total_cost_usd: number;
  num_turns: number;
  usage: {
    input_tokens: number;
    output_tokens: number;
    cache_read_input_tokens: number;
  };
}

export interface EvalContext {
  scenario: Scenario;
  invocationTranscript: string;
  invocationOutput: ClaudeJsonOutput | null;
  qualityTranscript: string | null;
  qualityOutput: ClaudeJsonOutput | null;
  skillName: string;
  skillDescription: string;
  skillContent: string;
  skillDir: string;
  workDir: string;
  invoked: boolean;
}

export interface EvalResult {
  evalId: string;
  name: string;
  score: number;
  reasoning: string;
  required?: boolean;
}

export interface Eval {
  id: string;
  name: string;
  evaluate(ctx: EvalContext): Promise<EvalResult>;
}
