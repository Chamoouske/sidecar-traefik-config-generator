import { describe, it, expect, vi, beforeEach } from 'vitest';
import { FileWriterService } from '../filesystem/FileWriterService';
import { ILogger } from '../core/interfaces/ILogger';
import { AppConfig } from '../types/config';
import { FileWriteError } from '../types/errors';

// Mock node:fs/promises
vi.mock('node:fs/promises', () => ({
  default: {
    writeFile: vi.fn(),
    readFile: vi.fn(),
    unlink: vi.fn(),
    mkdir: vi.fn(),
    readdir: vi.fn(),
    rename: vi.fn(),
    access: vi.fn(),
  },
  writeFile: vi.fn(),
  readFile: vi.fn(),
  unlink: vi.fn(),
  mkdir: vi.fn(),
  readdir: vi.fn(),
  rename: vi.fn(),
  access: vi.fn(),
}));

import * as fs from 'node:fs/promises';

const mockLogger: ILogger = {
  info: vi.fn(),
  warn: vi.fn(),
  error: vi.fn(),
  debug: vi.fn(),
  fatal: vi.fn(),
  child: () => mockLogger,
};

const mockConfig: AppConfig = {
  mode: 'all',
  node: { hostname: 'node1', ip: '192.168.1.1', nodeId: 'local-node' },
  docker: { socket: '/var/run/docker.sock', pollIntervalMs: 30000 },
  directories: {
    shared: '/data/shared',
    local: '/data/local',
    federation: '/data/shared/federation',
    middlewares: '/data/shared/middlewares',
    localGenerated: '/data/local/generated',
  },
  federation: {
    headerName: 'X-Federated',
    headerValue: 'true',
    defaultHealthCheckPath: '/',
    defaultHealthCheckInterval: '10s',
    defaultRetryAttempts: 3,
    defaultRetryInterval: '100ms',
    circuitBreakerThreshold: 0.3,
  },
  server: { port: 9090, healthEndpoint: '/health' },
  logging: { level: 'info', pretty: false },
};

describe('FileWriterService', () => {
  let fileWriter: FileWriterService;

  beforeEach(() => {
    vi.clearAllMocks();
    fileWriter = new FileWriterService(mockConfig, mockLogger);
  });

  describe('writeYaml', () => {
    it('should write YAML file atomically', async () => {
      const data = { http: { services: { test: {} } } };
      (fs.access as any).mockRejectedValue(new Error('ENOENT'));
      (fs.writeFile as any).mockResolvedValue(undefined);
      (fs.rename as any).mockResolvedValue(undefined);

      await fileWriter.writeYaml('/data/shared/federation/test.yaml', data);

      // Should have written to a temp file first
      expect(fs.writeFile).toHaveBeenCalledTimes(1);
      const writeCall = (fs.writeFile as any).mock.calls[0];
      expect(writeCall[0]).toContain('federation');
      expect(writeCall[0]).toContain('.tmp');
      expect(writeCall[1]).toContain('http');

      // Then rename to target
      expect(fs.rename).toHaveBeenCalledTimes(1);
      const renameCall = (fs.rename as any).mock.calls[0];
      expect(renameCall[1]).toContain('test.yaml');
    });

    it('should skip writing if content is unchanged', async () => {
      const data = { http: { services: { test: {} } } };
      // Return existing content that matches new content
      (fs.access as any).mockResolvedValue(undefined);
      (fs.readFile as any).mockResolvedValue(
        "http:\n  services:\n    test: {}\n",
      );

      await fileWriter.writeYaml('/data/shared/federation/test.yaml', data);

      // Should not write or rename
      expect(fs.writeFile).not.toHaveBeenCalled();
      expect(fs.rename).not.toHaveBeenCalled();
    });

    it('should create directory before writing', async () => {
      const data = { key: 'value' };
      (fs.access as any).mockRejectedValue(new Error('ENOENT'));
      (fs.writeFile as any).mockResolvedValue(undefined);
      (fs.rename as any).mockResolvedValue(undefined);

      await fileWriter.writeYaml('/data/shared/federation/new-test.yaml', data);

      // On Windows, path.resolve converts /data/... to c:\data\...
      // so just check that mkdir was called with the federation dir
      expect(fs.mkdir).toHaveBeenCalledTimes(1);
      expect(fs.mkdir).toHaveBeenCalledWith(
        expect.stringMatching(/(\/|\\)data(\/|\\)shared(\/|\\)federation/),
        { recursive: true },
      );
    });

    it('should throw FileWriteError on write failure', async () => {
      const data = { key: 'value' };
      (fs.access as any).mockRejectedValue(new Error('ENOENT'));
      (fs.writeFile as any).mockRejectedValue(new Error('Disk full'));

      await expect(
        fileWriter.writeYaml('/data/shared/federation/test.yaml', data),
      ).rejects.toThrow(FileWriteError);
    });

    it('should resolve relative paths to absolute', async () => {
      const data = { key: 'value' };
      (fs.access as any).mockRejectedValue(new Error('ENOENT'));
      (fs.writeFile as any).mockResolvedValue(undefined);
      (fs.rename as any).mockResolvedValue(undefined);

      await fileWriter.writeYaml('relative/path/test.yaml', data);

      // On Windows, path.resolve will prepend the cwd with backslashes
      const writeCall = (fs.writeFile as any).mock.calls[0];
      expect(writeCall[0]).toContain('sidecar');
      expect(writeCall[0]).toContain('relative');
    });
  });

  describe('readYaml', () => {
    it('should read and parse YAML file', async () => {
      const yamlContent = 'http:\n  services:\n    test:\n      loadBalancer:\n        servers:\n          - url: "http://localhost:3000"';
      (fs.readFile as any).mockResolvedValue(yamlContent);

      const result = await fileWriter.readYaml<any>(
        '/data/shared/federation/test.yaml',
      );

      expect(result).toBeDefined();
      expect(result.http.services.test.loadBalancer.servers[0].url).toBe(
        'http://localhost:3000',
      );
    });

    it('should throw FileWriteError if file does not exist', async () => {
      (fs.readFile as any).mockRejectedValue(
        Object.assign(new Error('ENOENT'), { code: 'ENOENT' }),
      );

      await expect(
        fileWriter.readYaml('/data/shared/federation/nonexistent.yaml'),
      ).rejects.toThrow(FileWriteError);
    });
  });

  describe('deleteFile', () => {
    it('should delete existing file', async () => {
      (fs.unlink as any).mockResolvedValue(undefined);

      await fileWriter.deleteFile('/data/shared/federation/test.yaml');

      expect(fs.unlink).toHaveBeenCalledWith(
        expect.stringMatching(
          /(\/|\\)data(\/|\\)shared(\/|\\)federation(\/|\\)test\.yaml/,
        ),
      );
    });

    it('should not throw when deleting non-existent file', async () => {
      (fs.unlink as any).mockRejectedValue(
        Object.assign(new Error('ENOENT'), { code: 'ENOENT' }),
      );

      await expect(
        fileWriter.deleteFile('/data/shared/federation/nonexistent.yaml'),
      ).resolves.toBeUndefined();
    });

    it('should throw FileWriteError on other unlink errors', async () => {
      (fs.unlink as any).mockRejectedValue(
        Object.assign(new Error('Permission denied'), { code: 'EACCES' }),
      );

      await expect(
        fileWriter.deleteFile('/data/shared/federation/test.yaml'),
      ).rejects.toThrow(FileWriteError);
    });
  });

  describe('ensureDirectory', () => {
    it('should create directory recursively', async () => {
      (fs.mkdir as any).mockResolvedValue(undefined);

      await fileWriter.ensureDirectory('/data/shared/federation');

      expect(fs.mkdir).toHaveBeenCalledWith(
        expect.stringMatching(/(\/|\\)data(\/|\\)shared(\/|\\)federation/),
        { recursive: true },
      );
    });

    it('should not throw when directory already exists', async () => {
      (fs.mkdir as any).mockResolvedValue(undefined);

      await expect(
        fileWriter.ensureDirectory('/data/shared/federation'),
      ).resolves.toBeUndefined();
    });

    it('should throw FileWriteError on mkdir failure', async () => {
      (fs.mkdir as any).mockRejectedValue(new Error('Read-only filesystem'));

      await expect(
        fileWriter.ensureDirectory('/data/shared/federation'),
      ).rejects.toThrow(FileWriteError);
    });
  });

  describe('listFiles', () => {
    it('should list files in directory', async () => {
      (fs.readdir as any).mockResolvedValue([
        { name: 'service1.yaml', isFile: () => true } as any,
        { name: 'service2.yaml', isFile: () => true } as any,
        { name: 'subdir', isFile: () => false } as any,
      ]);

      const files = await fileWriter.listFiles(
        '/data/shared/federation',
      );

      expect(files).toHaveLength(2);
      expect(files[0]).toContain('service1.yaml');
      expect(files[1]).toContain('service2.yaml');
    });

    it('should return full paths for files', async () => {
      (fs.readdir as any).mockResolvedValue([
        { name: 'test.yaml', isFile: () => true } as any,
      ]);

      const files = await fileWriter.listFiles(
        '/data/shared/federation',
      );

      expect(files[0]).toContain('federation');
      expect(files[0]).toContain('test.yaml');
    });

    it('should throw FileWriteError on readdir failure', async () => {
      (fs.readdir as any).mockRejectedValue(new Error('ENOENT'));

      await expect(
        fileWriter.listFiles('/data/shared/federation'),
      ).rejects.toThrow(FileWriteError);
    });
  });

  describe('exists', () => {
    it('should return true when file exists', async () => {
      (fs.access as any).mockResolvedValue(undefined);

      const result = await fileWriter.exists(
        '/data/shared/federation/test.yaml',
      );

      expect(result).toBe(true);
    });

    it('should return false when file does not exist', async () => {
      (fs.access as any).mockRejectedValue(new Error('ENOENT'));

      const result = await fileWriter.exists(
        '/data/shared/federation/nonexistent.yaml',
      );

      expect(result).toBe(false);
    });
  });
});
