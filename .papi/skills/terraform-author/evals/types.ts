export interface Scenario {
  id: string;
  prompt: string;
  fixtures?: Record<string, string>;
  tags?: string[];
  shouldInvoke?: boolean;
}

export interface EvalContext {
  scenario: Scenario;
  invocationTranscript: string;
  qualityTranscript: string | null;
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
