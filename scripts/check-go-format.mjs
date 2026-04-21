import { spawnSync } from 'node:child_process';

const result = spawnSync('gofmt', ['-l', '.'], { encoding: 'utf8' });

if (result.error) {
  console.error('Failed to run gofmt. Ensure the Go toolchain is installed.');
  console.error(result.error.message);
  process.exit(1);
}

if (result.status !== 0) {
  process.stderr.write(result.stderr);
  process.exit(result.status ?? 1);
}

const filesNeedingFormat = result.stdout
  .split(/\r?\n/)
  .map((line) => line.trim())
  .filter(Boolean)
  .filter((line) => line.endsWith('.go'));

if (filesNeedingFormat.length > 0) {
  console.error('Go files need formatting:');
  for (const file of filesNeedingFormat) {
    console.error(`- ${file}`);
  }
  process.exit(1);
}

console.log('Go formatting check passed.');

