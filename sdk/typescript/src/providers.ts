export const ENV_OPENAI_HOSTS = 'STAGEHAND_OPENAI_HOSTS';

const DEFAULT_OPENAI_HOSTS = new Set(['api.openai.com']);

export function configuredOpenAIHosts(env: NodeJS.ProcessEnv = process.env): Set<string> {
  const configured = new Set<string>();
  const raw = env[ENV_OPENAI_HOSTS] ?? '';
  for (const host of raw.split(',')) {
    const normalized = host.trim().toLowerCase();
    if (normalized.length > 0) {
      configured.add(normalized);
    }
  }

  return new Set([...DEFAULT_OPENAI_HOSTS, ...configured]);
}

export function isOpenAIHost(
  hostname: string | undefined,
  env: NodeJS.ProcessEnv = process.env
): boolean {
  if (hostname === undefined) {
    return false;
  }

  return configuredOpenAIHosts(env).has(hostname.trim().toLowerCase());
}

export function openAIOperationFromURL(
  method: string,
  url: URL | undefined,
  env: NodeJS.ProcessEnv = process.env
): string | undefined {
  if (url === undefined || !isOpenAIHost(url.hostname, env) || method !== 'POST') {
    return undefined;
  }

  if (url.pathname === '/v1/chat/completions') {
    return 'chat.completions.create';
  }

  if (url.pathname === '/v1/responses') {
    return 'responses.create';
  }

  return undefined;
}
