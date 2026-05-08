import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ConfigOrchestratorService } from '../services/ConfigOrchestratorService';
import { ISwarmDiscovery } from '../core/interfaces/ISwarmDiscovery';
import { IFederationStrategy } from '../core/interfaces/IFederationStrategy';
import { IMiddlewareGenerator } from '../core/interfaces/IMiddlewareGenerator';
import { IFileWriter } from '../core/interfaces/IFileWriter';
import { LocalConfigGeneratorService } from '../generators/LocalConfigGeneratorService';
import { ILogger } from '../core/interfaces/ILogger';
import { AppConfig } from '../types/config';
import { DiscoveredService } from '../types/docker';
import {
  FederationConfigOutput,
  LocalConfigOutput,
  MiddlewareConfigOutput,
} from '../types/federation';

const mockLogger: ILogger = {
  info: vi.fn(),
  warn: vi.fn(),
  error: vi.fn(),
  debug: vi.fn(),
  fatal: vi.fn(),
  child: () => mockLogger,
};

const mockConfig: AppConfig = {
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

function createMockService(name: string): DiscoveredService {
  return {
    serviceName: name,
    serviceId: `${name}-id`,
    labels: {
      'federation.enable': 'true',
      'federation.host': `${name}.local`,
      'federation.port': '3000',
    },
    endpoints: [
      {
        nodeId: 'local-node',
        nodeHostname: 'node1',
        nodeIp: '192.168.1.1',
        taskStatus: 'running',
        taskId: `task-${name}`,
      },
    ],
  };
}

describe('ConfigOrchestratorService', () => {
  let mockDiscovery: ISwarmDiscovery;
  let mockFederationGenerator: IFederationStrategy;
  let mockLocalGenerator: LocalConfigGeneratorService;
  let mockMiddlewareGenerator: IMiddlewareGenerator;
  let mockFileWriter: IFileWriter;
  let orchestrator: ConfigOrchestratorService;

  beforeEach(() => {
    vi.clearAllMocks();

    mockDiscovery = {
      discoverAllServices: vi.fn() as any,
      discoverLocalServices: vi.fn(),
      getCurrentNodeId: vi.fn().mockReturnValue('local-node'),
    };

    mockFederationGenerator = {
      canHandle: vi.fn() as any,
      generate: vi.fn() as any,
    };

    // We need a real LocalConfigGeneratorService or a mock
    // Since it's a class (not an interface), we mock it at instance level
    mockLocalGenerator = {
      canGenerate: vi.fn() as any,
      generate: vi.fn() as any,
    } as unknown as LocalConfigGeneratorService;

    mockMiddlewareGenerator = {
      generate: vi.fn() as any,
    };

    mockFileWriter = {
      writeYaml: vi.fn(),
      readYaml: vi.fn(),
      deleteFile: vi.fn(),
      ensureDirectory: vi.fn(),
      listFiles: vi.fn() as any,
      exists: vi.fn() as any,
    };

    orchestrator = new ConfigOrchestratorService(
      mockDiscovery,
      mockFederationGenerator,
      mockLocalGenerator,
      mockMiddlewareGenerator,
      mockFileWriter,
      mockConfig,
      mockLogger,
    );
  });

  describe('runGenerationCycle', () => {
    it('should call discovery and generate configs for services', async () => {
      const services = [
        createMockService('app-1'),
        createMockService('app-2'),
      ];

      mockDiscovery.discoverAllServices.mockResolvedValue(services);
      mockFederationGenerator.canHandle.mockReturnValue(true);
      mockFederationGenerator.generate.mockResolvedValue({
        http: {
          services: {
            'app-1': {
              loadBalancer: {
                passHostHeader: true,
                servers: [{ url: 'http://192.168.1.1:3000' }],
              },
            },
          },
        },
      });
      mockMiddlewareGenerator.generate.mockResolvedValue({
        http: {
          middlewares: {
            'app-1-retry': {
              retry: { attempts: 3, initialInterval: '100ms' },
            },
          },
        },
      });
      mockLocalGenerator.canGenerate.mockReturnValue(true);
      mockLocalGenerator.generate.mockResolvedValue({
        http: {
          services: {
            'app-1-local': {
              loadBalancer: {
                passHostHeader: true,
                servers: [{ url: 'http://app-1:3000' }],
              },
            },
          },
          routers: {
            'app-1-local': {
              rule: 'Host(`app-1.local`)',
              service: 'app-1-local',
              entryPoints: ['websecure'],
            },
          },
        },
      });
      mockFileWriter.listFiles.mockResolvedValue([]);

      await orchestrator.runGenerationCycle();

      expect(mockDiscovery.discoverAllServices).toHaveBeenCalledTimes(1);
      expect(mockFederationGenerator.canHandle).toHaveBeenCalled();
      expect(mockFederationGenerator.generate).toHaveBeenCalled();
      expect(mockMiddlewareGenerator.generate).toHaveBeenCalled();
      expect(mockLocalGenerator.canGenerate).toHaveBeenCalled();
    });

    it('should handle empty services list gracefully', async () => {
      mockDiscovery.discoverAllServices.mockResolvedValue([]);

      await orchestrator.runGenerationCycle();

      expect(mockFederationGenerator.generate).not.toHaveBeenCalled();
      expect(mockMiddlewareGenerator.generate).not.toHaveBeenCalled();
      expect(mockFileWriter.writeYaml).not.toHaveBeenCalled();
    });

    it('should handle discovery errors gracefully', async () => {
      mockDiscovery.discoverAllServices.mockRejectedValue(
        new Error('Discovery failed'),
      );

      await orchestrator.runGenerationCycle();

      // Should log error but not throw
      expect(mockLogger.error).toHaveBeenCalled();
    });

    it('should write federation config to correct path', async () => {
      const services = [createMockService('app-1')];

      mockDiscovery.discoverAllServices.mockResolvedValue(services);
      mockFederationGenerator.canHandle.mockReturnValue(true);
      mockFederationGenerator.generate.mockResolvedValue({
        http: {
          services: {
            'app-1': {
              loadBalancer: {
                passHostHeader: true,
                servers: [{ url: 'http://192.168.1.1:3000' }],
              },
            },
          },
        },
      });
      mockMiddlewareGenerator.generate.mockResolvedValue(null);
      mockLocalGenerator.canGenerate.mockReturnValue(false);
      mockFileWriter.listFiles.mockResolvedValue([]);

      await orchestrator.runGenerationCycle();

      expect(mockFileWriter.writeYaml).toHaveBeenCalledWith(
        '/data/shared/federation/app-1.yaml',
        expect.any(Object),
      );
    });

    it('should write middleware config to correct path', async () => {
      const services = [createMockService('app-1')];

      mockDiscovery.discoverAllServices.mockResolvedValue(services);
      mockFederationGenerator.canHandle.mockReturnValue(false);
      mockMiddlewareGenerator.generate.mockResolvedValue({
        http: {
          middlewares: {
            'app-1-retry': {
              retry: { attempts: 3, initialInterval: '100ms' },
            },
          },
        },
      });
      mockLocalGenerator.canGenerate.mockReturnValue(false);
      mockFileWriter.listFiles.mockResolvedValue([]);

      await orchestrator.runGenerationCycle();

      expect(mockFileWriter.writeYaml).toHaveBeenCalledWith(
        '/data/shared/middlewares/app-1.yaml',
        expect.any(Object),
      );
    });

    it('should write local config to correct path', async () => {
      const services = [createMockService('app-1')];

      mockDiscovery.discoverAllServices.mockResolvedValue(services);
      mockFederationGenerator.canHandle.mockReturnValue(true);
      mockFederationGenerator.generate.mockResolvedValue({
        http: { services: {} },
      } as any);
      mockMiddlewareGenerator.generate.mockResolvedValue(null);
      mockLocalGenerator.canGenerate.mockReturnValue(true);
      mockLocalGenerator.generate.mockResolvedValue({
        http: {
          services: {
            'app-1-local': {
              loadBalancer: {
                passHostHeader: true,
                servers: [{ url: 'http://app-1:3000' }],
              },
            },
          },
          routers: {
            'app-1-local': {
              rule: 'Host(`app-1.local`)',
              service: 'app-1-local',
              entryPoints: ['websecure'],
            },
          },
        },
      });
      mockFileWriter.listFiles.mockResolvedValue([]);

      await orchestrator.runGenerationCycle();

      expect(mockFileWriter.writeYaml).toHaveBeenCalledWith(
        '/data/local/generated/app-1.yaml',
        expect.any(Object),
      );
    });
  });

  describe('cleanupStaleFiles', () => {
    it('should remove files for services that no longer exist', async () => {
      const services = [createMockService('app-1')];

      mockDiscovery.discoverAllServices.mockResolvedValue(services);
      mockFederationGenerator.canHandle.mockReturnValue(false);
      mockMiddlewareGenerator.generate.mockResolvedValue(null);
      mockLocalGenerator.canGenerate.mockReturnValue(false);
      mockFileWriter.listFiles.mockImplementation(
        async (dirPath: string) => {
          if (dirPath.includes('federation')) {
            return [
              '/data/shared/federation/app-1.yaml',
              '/data/shared/federation/app-2.yaml',
              '/data/shared/federation/app-3.yaml',
            ];
          }
          return [];
        },
      );

      await orchestrator.runGenerationCycle();

      // Should delete app-2.yaml and app-3.yaml (stale), keep app-1.yaml
      expect(mockFileWriter.deleteFile).toHaveBeenCalledTimes(2);
      expect(mockFileWriter.deleteFile).toHaveBeenCalledWith(
        '/data/shared/federation/app-2.yaml',
      );
      expect(mockFileWriter.deleteFile).toHaveBeenCalledWith(
        '/data/shared/federation/app-3.yaml',
      );
    });

    it('should handle directory listing errors gracefully', async () => {
      const services = [createMockService('app-1')];

      mockDiscovery.discoverAllServices.mockResolvedValue(services);
      mockFederationGenerator.canHandle.mockReturnValue(false);
      mockMiddlewareGenerator.generate.mockResolvedValue(null);
      mockLocalGenerator.canGenerate.mockReturnValue(false);
      mockFileWriter.listFiles.mockRejectedValue(
        Object.assign(new Error('ENOENT'), { code: 'ENOENT' }),
      );

      await expect(
        orchestrator.runGenerationCycle(),
      ).resolves.toBeUndefined();
    });

    it('should remove stale local config when service is no longer local', async () => {
      const services = [createMockService('app-1')];

      mockDiscovery.discoverAllServices.mockResolvedValue(services);
      mockFederationGenerator.canHandle.mockReturnValue(false);
      mockMiddlewareGenerator.generate.mockResolvedValue(null);
      mockLocalGenerator.canGenerate.mockReturnValue(false);
      mockFileWriter.listFiles.mockResolvedValue([]);
      mockFileWriter.exists.mockResolvedValue(true);

      await orchestrator.runGenerationCycle();

      // Should delete the old local config since service is no longer local
      expect(mockFileWriter.deleteFile).toHaveBeenCalledWith(
        '/data/local/generated/app-1.yaml',
      );
    });

    it('should not call deleteFile if local file does not exist', async () => {
      const services = [createMockService('app-1')];

      mockDiscovery.discoverAllServices.mockResolvedValue(services);
      mockFederationGenerator.canHandle.mockReturnValue(false);
      mockMiddlewareGenerator.generate.mockResolvedValue(null);
      mockLocalGenerator.canGenerate.mockReturnValue(false);
      mockFileWriter.listFiles.mockResolvedValue([]);
      mockFileWriter.exists.mockResolvedValue(false);

      await orchestrator.runGenerationCycle();

      // Should NOT call deleteFile since file doesn't exist
      expect(mockFileWriter.deleteFile).not.toHaveBeenCalledWith(
        '/data/local/generated/app-1.yaml',
      );
    });
  });
});
