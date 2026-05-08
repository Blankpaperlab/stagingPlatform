import fs from 'node:fs';

export type ServiceMapping = {
  name: string;
  host: string;
  pathPrefix: string;
  allowedTiers: number[];
  ignoreRequestPaths: string[];
  ignoreResponsePaths: string[];
};

export function loadServiceMappings(configPath: string | undefined): ServiceMapping[] {
  if (configPath === undefined || !fs.existsSync(configPath)) {
    return [];
  }

  const raw = fs.readFileSync(configPath, 'utf8');
  return parseServices(raw)
    .map((service) => {
      const match = asRecord(service.match);
      const name = asString(service.name).trim();
      const host = asString(match?.host).trim().toLowerCase();
      if (name.length === 0 || host.length === 0) {
        return undefined;
      }

      const rawPathPrefix = asString(match?.path_prefix).trim();
      const pathPrefix =
        rawPathPrefix.length === 0
          ? '/'
          : rawPathPrefix.startsWith('/')
            ? rawPathPrefix
            : `/${rawPathPrefix}`;
      const ignore = asRecord(service.ignore);
      const replay = asRecord(service.replay);
      return {
        name,
        host,
        pathPrefix,
        allowedTiers: numberArray(replay?.allowed_tiers, [0]),
        ignoreRequestPaths: stringArray(ignore?.request_paths),
        ignoreResponsePaths: stringArray(ignore?.response_paths),
      };
    })
    .filter((mapping): mapping is ServiceMapping => mapping !== undefined)
    .sort((left, right) => {
      const hostOrder = right.host.localeCompare(left.host);
      if (hostOrder !== 0) {
        return hostOrder;
      }
      const prefixOrder = right.pathPrefix.length - left.pathPrefix.length;
      if (prefixOrder !== 0) {
        return prefixOrder;
      }
      return right.name.localeCompare(left.name);
    });
}

export function mappedServiceName(
  url: URL | undefined,
  serviceMappings: readonly ServiceMapping[]
): string | undefined {
  if (url === undefined) {
    return undefined;
  }

  const host = url.hostname.toLowerCase();
  for (const mapping of serviceMappings) {
    if (mapping.host === host && url.pathname.startsWith(mapping.pathPrefix)) {
      return mapping.name;
    }
  }
  return undefined;
}

function parseServices(raw: string): Array<Record<string, unknown>> {
  const services: Array<Record<string, unknown>> = [];
  let current: Record<string, unknown> | undefined;
  let section: string | undefined;
  let listKey: string | undefined;
  let inServices = false;

  for (const rawLine of raw.split(/\r?\n/)) {
    const line = stripComment(rawLine).trimEnd();
    if (line.trim().length === 0) {
      continue;
    }

    const indent = line.length - line.trimStart().length;
    const stripped = line.trim();
    if (indent === 0) {
      inServices = stripped === 'services:';
      current = undefined;
      section = undefined;
      listKey = undefined;
      continue;
    }

    if (!inServices) {
      continue;
    }

    if (indent === 2 && stripped.startsWith('- ')) {
      current = {};
      services.push(current);
      section = undefined;
      listKey = undefined;
      const remainder = stripped.slice(2).trim();
      if (remainder.length > 0) {
        const [key, value] = splitKeyValue(remainder);
        if (key !== undefined) {
          current[key] = value;
        }
      }
      continue;
    }

    if (current === undefined) {
      continue;
    }

    if (indent === 4) {
      const [key, value] = splitKeyValue(stripped);
      if (key === undefined) {
        continue;
      }
      if (value === undefined) {
        section = key;
        current[section] = {};
        listKey = undefined;
        continue;
      }
      section = undefined;
      listKey = undefined;
      current[key] = value;
      continue;
    }

    if (indent === 6 && section !== undefined) {
      const parent = asRecord(current[section]);
      if (parent === undefined) {
        continue;
      }
      const [key, value] = splitKeyValue(stripped);
      if (key !== undefined) {
        if (value === undefined) {
          parent[key] = [];
          listKey = key;
        } else {
          parent[key] = value;
          listKey = undefined;
        }
      }
      continue;
    }

    if (
      indent === 8 &&
      section !== undefined &&
      listKey !== undefined &&
      stripped.startsWith('- ')
    ) {
      const parent = asRecord(current[section]);
      const items = parent?.[listKey];
      if (Array.isArray(items)) {
        items.push(parseScalar(stripped.slice(2).trim()));
      }
    }
  }

  return services;
}

function splitKeyValue(value: string): [string | undefined, unknown] {
  const separator = value.indexOf(':');
  if (separator === -1) {
    return [undefined, undefined];
  }

  const key = value.slice(0, separator).trim();
  const raw = value.slice(separator + 1).trim();
  if (key.length === 0) {
    return [undefined, undefined];
  }
  if (raw.length === 0) {
    return [key, undefined];
  }
  return [key, parseScalar(raw)];
}

function parseScalar(value: string): unknown {
  if (value.startsWith('[') && value.endsWith(']')) {
    const inner = value.slice(1, -1).trim();
    if (inner.length === 0) {
      return [];
    }
    return inner.split(',').map((item) => parseScalar(item.trim()));
  }
  if (
    (value.startsWith('"') && value.endsWith('"')) ||
    (value.startsWith("'") && value.endsWith("'"))
  ) {
    return value.slice(1, -1);
  }
  if (/^\d+$/.test(value)) {
    return Number.parseInt(value, 10);
  }
  return value;
}

function stripComment(line: string): string {
  let inSingle = false;
  let inDouble = false;
  for (let index = 0; index < line.length; index += 1) {
    const char = line[index];
    if (char === "'" && !inDouble) {
      inSingle = !inSingle;
    } else if (char === '"' && !inSingle) {
      inDouble = !inDouble;
    } else if (char === '#' && !inSingle && !inDouble) {
      return line.slice(0, index);
    }
  }
  return line;
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
    return value as Record<string, unknown>;
  }
  return undefined;
}

function asString(value: unknown): string {
  if (value === undefined || value === null) {
    return '';
  }
  return String(value);
}

function stringArray(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.map((item) => String(item).trim()).filter((item) => item.length > 0);
}

function numberArray(value: unknown, fallback: number[]): number[] {
  if (!Array.isArray(value)) {
    return fallback;
  }
  const numbers = value.filter((item): item is number => Number.isInteger(item));
  return numbers.length === 0 ? fallback : Array.from(new Set(numbers));
}
