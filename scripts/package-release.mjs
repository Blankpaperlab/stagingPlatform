import { existsSync, mkdirSync, readdirSync, readFileSync, rmSync } from 'node:fs';
import { basename, dirname, join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const distDir = join(repoRoot, 'dist', 'release');
const version = readFileSync(join(repoRoot, 'VERSION'), 'utf8').trim();
const platform = process.platform;
const arch = process.arch;
const exe = platform === 'win32' ? '.exe' : '';

function run(command, args, options = {}) {
  const completed = spawnSync(command, args, {
    cwd: repoRoot,
    stdio: 'inherit',
    shell: platform === 'win32',
    ...options,
  });
  if (completed.status !== 0) {
    throw new Error(`${command} ${args.join(' ')} failed with status ${completed.status}`);
  }
}

function readJSON(path) {
  return JSON.parse(readFileSync(path, 'utf8'));
}

function assertVersion(name, actual, expected) {
  if (actual !== expected) {
    throw new Error(`${name} version ${actual} does not match ${expected}`);
  }
}

function pyProjectVersion() {
  const pyproject = readFileSync(join(repoRoot, 'sdk', 'python', 'pyproject.toml'), 'utf8');
  const match = pyproject.match(/^version\s*=\s*"([^"]+)"/m);
  if (!match) {
    throw new Error('could not read Python project version');
  }
  return match[1];
}

function pythonVersionFromArtifactVersion(value) {
  return value.replace('-alpha.', 'a');
}

function main() {
  assertVersion('root package', readJSON(join(repoRoot, 'package.json')).version, version);
  assertVersion(
    'TypeScript SDK',
    readJSON(join(repoRoot, 'sdk', 'typescript', 'package.json')).version,
    version
  );
  assertVersion('Python SDK', pyProjectVersion(), pythonVersionFromArtifactVersion(version));

  rmSync(distDir, { recursive: true, force: true });
  mkdirSync(distDir, { recursive: true });

  const cliDir = join(distDir, `stagehand-${version}-${platform}-${arch}`);
  mkdirSync(cliDir, { recursive: true });
  run('go', ['build', '-o', join(cliDir, `stagehand${exe}`), './cmd/stagehand']);
  run('go', ['build', '-o', join(cliDir, `stagehandd${exe}`), './cmd/stagehandd']);

  run('python', ['-m', 'build', 'sdk/python']);
  run('npm', ['run', '-w', '@stagehand/sdk', 'build']);
  run('npm', ['pack', '--workspace', '@stagehand/sdk', '--pack-destination', distDir]);

  const pythonDist = join(repoRoot, 'sdk', 'python', 'dist');
  const wheels = readdirSync(pythonDist).filter((name) => name.endsWith('.whl'));
  const sdists = readdirSync(pythonDist).filter((name) => name.endsWith('.tar.gz'));
  const npmPacks = readdirSync(distDir).filter((name) => name.endsWith('.tgz'));

  if (
    !existsSync(join(cliDir, `stagehand${exe}`)) ||
    !existsSync(join(cliDir, `stagehandd${exe}`))
  ) {
    throw new Error('Go CLI release binaries were not created');
  }
  if (wheels.length === 0 || sdists.length === 0) {
    throw new Error('Python wheel and sdist were not created');
  }
  if (npmPacks.length === 0) {
    throw new Error('TypeScript npm package was not created');
  }

  console.log('Release artifacts verified:');
  console.log(`- ${cliDir}`);
  console.log(`- ${join(pythonDist, basename(wheels.at(-1)))}`);
  console.log(`- ${join(pythonDist, basename(sdists.at(-1)))}`);
  console.log(`- ${join(distDir, basename(npmPacks.at(-1)))}`);
}

main();
