import { describe, it, expect, vi, beforeEach } from 'vitest';
import { LocalOrchestratorService } from '../orchestration/LocalOrchestratorService.js';
import { ILocalDiscoveryService } from '../core/interfaces/ILocalDiscoveryService.js';
import { ILocalConfigGenerator } from '../core/interfaces/ILocalConfigGenerator.js';
import { IFileWriter } from '../core/interfaces/IFileWriter.js';
import { ILogger } from '../core/interfaces/ILogger.js';
import { AppConfig } from '../types/config.js';
import { DiscoveredService } from '../types/docker.js';

const mockLogger: ILogger = {
    info: vi.fn(),
    warn: vi.fn(),
    error: vi.fn(),
    debug: vi.fn(),
    fatal: vi.fn(),
    child: () => mockLogger,
};

describe('LocalOrchestratorService', () => {
    const mockDiscovery: ILocalDiscoveryService = {
        discoverLocalServices: vi.fn(),
        getCurrentNodeId: vi.fn(),
    };

    const mockLocalGenerator: ILocalConfigGenerator = {
        canGenerate: vi.fn(),
        generate: vi.fn(),
    };

    const mockFileWriter: IFileWriter = {
        writeYaml: vi.fn(),
        readYaml: vi.fn(),
        deleteFile: vi.fn(),
        ensureDirectory: vi.fn(),
        listFiles: vi.fn(),
        exists: vi.fn(),
    };

    const mockConfig: AppConfig = {
        mode: 'local',
        node: { hostname: 'node1', ip: '192.168.1.1', nodeId: 'node-1' },
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
            circuitBreakerThreshold: 0.30,
        },
        server: { port: 9090, healthEndpoint: '/health' },
        logging: { level: 'info', pretty: false },
    };

    const orchestrator = new LocalOrchestratorService(
        mockDiscovery,
        mockLocalGenerator,
        mockFileWriter,
        mockConfig,
        mockLogger,
    );

    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('should discover local services and generate configs', async () => {
        const services: DiscoveredService[] = [
            {
                serviceName: 'app1',
                serviceId: 'svc-1',
                labels: { 'federation.enable': 'true' },
                endpoints: [{ nodeId: 'node-1', nodeHostname: 'node1', nodeIp: '192.168.1.1', taskStatus: 'running', taskId: 'task-1' }],
            },
        ];

        vi.mocked(mockDiscovery.discoverLocalServices).mockResolvedValue(services);
        vi.mocked(mockLocalGenerator.generate).mockResolvedValue({
            http: {
                services: {
                    'app1-local': {
                        loadBalancer: { passHostHeader: true, servers: [{ url: 'http://app1:3000' }] },
                    },
                },
                routers: {
                    'app1-local': {
                        rule: 'Host(`app1.local`)',
                        service: 'app1-local',
                        entryPoints: ['websecure'],
                    },
                },
            },
        });
        vi.mocked(mockFileWriter.listFiles).mockResolvedValue([]);

        await orchestrator.runGenerationCycle();

        expect(mockDiscovery.discoverLocalServices).toHaveBeenCalledOnce();
        expect(mockLocalGenerator.generate).toHaveBeenCalledOnce();
        expect(mockFileWriter.writeYaml).toHaveBeenCalledWith(
            '/data/local/generated/app1.yaml',
            expect.any(Object),
        );
    });

    it('should skip service when local config is null', async () => {
        const services: DiscoveredService[] = [
            {
                serviceName: 'remote-app',
                serviceId: 'svc-1',
                labels: {},
                endpoints: [],
            },
        ];

        vi.mocked(mockDiscovery.discoverLocalServices).mockResolvedValue(services);
        vi.mocked(mockLocalGenerator.generate).mockResolvedValue(null);
        vi.mocked(mockFileWriter.listFiles).mockResolvedValue([]);

        await orchestrator.runGenerationCycle();

        // Should NOT call writeYaml because config is null
        expect(mockFileWriter.writeYaml).not.toHaveBeenCalled();
        // Should still check for stale files
        expect(mockFileWriter.listFiles).toHaveBeenCalledWith('/data/local/generated');
    });

    it('should handle empty services list gracefully', async () => {
        vi.mocked(mockDiscovery.discoverLocalServices).mockResolvedValue([]);

        await orchestrator.runGenerationCycle();

        expect(mockLocalGenerator.generate).not.toHaveBeenCalled();
        expect(mockFileWriter.writeYaml).not.toHaveBeenCalled();
    });

    it('should cleanup stale local files', async () => {
        const services: DiscoveredService[] = [
            {
                serviceName: 'active-app',
                serviceId: 'svc-1',
                labels: { 'federation.enable': 'true' },
                endpoints: [{ nodeId: 'node-1', nodeHostname: 'node1', nodeIp: '192.168.1.1', taskStatus: 'running', taskId: 'task-1' }],
            },
        ];

        vi.mocked(mockDiscovery.discoverLocalServices).mockResolvedValue(services);
        vi.mocked(mockLocalGenerator.generate).mockResolvedValue({
            http: {
                services: {
                    'active-app-local': {
                        loadBalancer: { passHostHeader: true, servers: [{ url: 'http://active-app:3000' }] },
                    },
                },
                routers: {
                    'active-app-local': {
                        rule: 'Host(`active.local`)',
                        service: 'active-app-local',
                        entryPoints: ['websecure'],
                    },
                },
            },
        });
        vi.mocked(mockFileWriter.listFiles).mockResolvedValue([
            '/data/local/generated/active-app.yaml',
            '/data/local/generated/stale-app.yaml',
        ]);

        await orchestrator.runGenerationCycle();

        expect(mockFileWriter.deleteFile).toHaveBeenCalledWith(
            '/data/local/generated/stale-app.yaml',
        );
        expect(mockFileWriter.deleteFile).not.toHaveBeenCalledWith(
            '/data/local/generated/active-app.yaml',
        );
    });

    it('should handle errors in service generation without stopping', async () => {
        const services: DiscoveredService[] = [
            {
                serviceName: 'good-app',
                serviceId: 'svc-1',
                labels: { 'federation.enable': 'true' },
                endpoints: [{ nodeId: 'node-1', nodeHostname: 'node1', nodeIp: '192.168.1.1', taskStatus: 'running', taskId: 'task-1' }],
            },
            {
                serviceName: 'bad-app',
                serviceId: 'svc-2',
                labels: { 'federation.enable': 'true' },
                endpoints: [{ nodeId: 'node-1', nodeHostname: 'node1', nodeIp: '192.168.1.1', taskStatus: 'running', taskId: 'task-2' }],
            },
        ];

        vi.mocked(mockDiscovery.discoverLocalServices).mockResolvedValue(services);
        vi.mocked(mockLocalGenerator.generate)
            .mockResolvedValueOnce({
                http: {
                    services: {
                        'good-app-local': {
                            loadBalancer: { passHostHeader: true, servers: [{ url: 'http://good-app:3000' }] },
                        },
                    },
                    routers: {
                        'good-app-local': {
                            rule: 'Host(`good.local`)',
                            service: 'good-app-local',
                            entryPoints: ['websecure'],
                        },
                    },
                },
            })
            .mockRejectedValueOnce(new Error('Generation failed'));
        vi.mocked(mockFileWriter.listFiles).mockResolvedValue([]);

        await orchestrator.runGenerationCycle();

        // Should have written good-app but not bad-app
        expect(mockFileWriter.writeYaml).toHaveBeenCalledTimes(1);
        expect(mockFileWriter.writeYaml).toHaveBeenCalledWith(
            '/data/local/generated/good-app.yaml',
            expect.any(Object),
        );
    });

    it('should handle ENOENT errors gracefully during cleanup', async () => {
        vi.mocked(mockDiscovery.discoverLocalServices).mockResolvedValue([]);
        vi.mocked(mockFileWriter.listFiles).mockRejectedValue(
            Object.assign(new Error('ENOENT'), { code: 'ENOENT' }),
        );

        await expect(
            orchestrator.runGenerationCycle(),
        ).resolves.toBeUndefined();
    });

    it('should skip service and clean stale file when local generator returns null', async () => {
        const services: DiscoveredService[] = [
            {
                serviceName: 'no-service-config',
                serviceId: 'svc-1',
                labels: { 'federation.enable': 'true' },
                endpoints: [{ nodeId: 'node-1', nodeHostname: 'node1', nodeIp: '192.168.1.1', taskStatus: 'running', taskId: 'task-1' }],
            },
        ];

        vi.mocked(mockDiscovery.discoverLocalServices).mockResolvedValue(services);
        // Generator returns null (e.g., not a local service or invalid labels)
        vi.mocked(mockLocalGenerator.generate).mockResolvedValue(null);
        vi.mocked(mockFileWriter.listFiles).mockResolvedValue([
            '/data/local/generated/no-service-config.yaml',
        ]);

        await orchestrator.runGenerationCycle();

        // Service returned null from generator, so writeYaml is NOT called
        expect(mockFileWriter.writeYaml).not.toHaveBeenCalled();
        // The stale file should be removed since processedServices doesn't include it
        expect(mockFileWriter.deleteFile).toHaveBeenCalledWith(
            '/data/local/generated/no-service-config.yaml',
        );
    });
});
