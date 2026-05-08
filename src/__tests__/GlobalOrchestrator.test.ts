import { describe, it, expect, vi, beforeEach } from 'vitest';
import { GlobalOrchestratorService } from '../orchestration/GlobalOrchestratorService.js';
import { ISwarmDiscovery } from '../core/interfaces/ISwarmDiscovery.js';
import { IFederationStrategy } from '../core/interfaces/IFederationStrategy.js';
import { IMiddlewareGenerator } from '../core/interfaces/IMiddlewareGenerator.js';
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

describe('GlobalOrchestratorService', () => {
    const mockDiscovery: ISwarmDiscovery = {
        discoverAllServices: vi.fn(),
        getCurrentNodeId: vi.fn(),
    };

    const mockFederationGen: IFederationStrategy = {
        canHandle: vi.fn(),
        generate: vi.fn(),
    };

    const mockMiddlewareGen: IMiddlewareGenerator = {
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
        mode: 'all',
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

    beforeEach(() => {
        vi.clearAllMocks();
    });

    it('should discover and generate configs for all services', async () => {
        const services: DiscoveredService[] = [
            {
                serviceName: 'app1',
                serviceId: 'svc-1',
                labels: {
                    'federation.enable': 'true',
                    'federation.host': 'app.local',
                    'federation.port': '3000',
                },
                endpoints: [
                    {
                        nodeId: 'node-1',
                        nodeHostname: 'node1',
                        nodeIp: '192.168.1.1',
                        taskStatus: 'running',
                        taskId: 'task-1',
                    },
                ],
            },
        ];

        vi.mocked(mockDiscovery.discoverAllServices).mockResolvedValue(services);
        vi.mocked(mockFederationGen.canHandle).mockReturnValue(true);
        vi.mocked(mockFederationGen.generate).mockResolvedValue({
            http: {
                services: {
                    app1: {
                        loadBalancer: {
                            passHostHeader: true,
                            servers: [{ url: 'http://192.168.1.1:3000' }],
                        },
                    },
                },
            },
        });
        vi.mocked(mockMiddlewareGen.generate).mockResolvedValue(null);
        vi.mocked(mockFileWriter.listFiles).mockResolvedValue([]);

        const orchestrator = new GlobalOrchestratorService(
            mockDiscovery,
            mockFederationGen,
            mockMiddlewareGen,
            mockFileWriter,
            mockConfig,
            mockLogger,
        );

        await orchestrator.runGenerationCycle();

        expect(mockDiscovery.discoverAllServices).toHaveBeenCalledOnce();
        expect(mockFederationGen.canHandle).toHaveBeenCalledOnce();
        expect(mockFederationGen.generate).toHaveBeenCalledOnce();
        expect(mockFileWriter.writeYaml).toHaveBeenCalledWith(
            '/data/shared/federation/app1.yaml',
            expect.any(Object),
        );
    });

    it('should handle discovery returning no services', async () => {
        vi.mocked(mockDiscovery.discoverAllServices).mockResolvedValue([]);
        vi.mocked(mockFileWriter.listFiles).mockResolvedValue([]);

        const orchestrator = new GlobalOrchestratorService(
            mockDiscovery,
            mockFederationGen,
            mockMiddlewareGen,
            mockFileWriter,
            mockConfig,
            mockLogger,
        );

        await orchestrator.runGenerationCycle();
        expect(mockFederationGen.generate).not.toHaveBeenCalled();
        expect(mockFileWriter.writeYaml).not.toHaveBeenCalled();
    });

    it('should cleanup stale files', async () => {
        const services: DiscoveredService[] = [
            {
                serviceName: 'active-app',
                serviceId: 'svc-1',
                labels: {
                    'federation.enable': 'true',
                    'federation.host': 'active.local',
                    'federation.port': '8080',
                },
                endpoints: [
                    {
                        nodeId: 'node-1',
                        nodeHostname: 'node1',
                        nodeIp: '192.168.1.1',
                        taskStatus: 'running',
                        taskId: 'task-1',
                    },
                ],
            },
        ];

        vi.mocked(mockDiscovery.discoverAllServices).mockResolvedValue(services);
        vi.mocked(mockFederationGen.canHandle).mockReturnValue(true);
        vi.mocked(mockFederationGen.generate).mockResolvedValue({
            http: {
                services: {
                    'active-app': {
                        loadBalancer: { passHostHeader: true, servers: [] },
                    },
                },
            },
        });
        vi.mocked(mockMiddlewareGen.generate).mockResolvedValue(null);
        vi.mocked(mockFileWriter.listFiles).mockResolvedValue([
            '/data/shared/federation/active-app.yaml',
            '/data/shared/federation/stale-app.yaml',
        ]);

        const orchestrator = new GlobalOrchestratorService(
            mockDiscovery,
            mockFederationGen,
            mockMiddlewareGen,
            mockFileWriter,
            mockConfig,
            mockLogger,
        );

        await orchestrator.runGenerationCycle();

        // Should delete stale-app.yaml but keep active-app.yaml
        expect(mockFileWriter.deleteFile).toHaveBeenCalledWith(
            '/data/shared/federation/stale-app.yaml',
        );
        expect(mockFileWriter.deleteFile).not.toHaveBeenCalledWith(
            '/data/shared/federation/active-app.yaml',
        );
    });

    it('should handle errors in service generation without stopping', async () => {
        const services: DiscoveredService[] = [
            {
                serviceName: 'good-app',
                serviceId: 'svc-1',
                labels: {
                    'federation.enable': 'true',
                    'federation.host': 'good.local',
                    'federation.port': '8080',
                },
                endpoints: [],
            },
            {
                serviceName: 'bad-app',
                serviceId: 'svc-2',
                labels: {
                    'federation.enable': 'true',
                    'federation.host': 'bad.local',
                    'federation.port': '8080',
                },
                endpoints: [],
            },
        ];

        vi.mocked(mockDiscovery.discoverAllServices).mockResolvedValue(services);
        vi.mocked(mockFederationGen.canHandle).mockReturnValue(true);
        vi.mocked(mockFederationGen.generate)
            .mockResolvedValueOnce({
                http: {
                    services: {
                        'good-app': {
                            loadBalancer: { passHostHeader: true, servers: [] },
                        },
                    },
                },
            })
            .mockRejectedValueOnce(new Error('Generation failed'));
        vi.mocked(mockMiddlewareGen.generate).mockResolvedValue(null);
        vi.mocked(mockFileWriter.listFiles).mockResolvedValue([]);

        const orchestrator = new GlobalOrchestratorService(
            mockDiscovery,
            mockFederationGen,
            mockMiddlewareGen,
            mockFileWriter,
            mockConfig,
            mockLogger,
        );

        await orchestrator.runGenerationCycle();

        // Should have written good-app but not bad-app
        expect(mockFileWriter.writeYaml).toHaveBeenCalledTimes(1);
        expect(mockFileWriter.writeYaml).toHaveBeenCalledWith(
            '/data/shared/federation/good-app.yaml',
            expect.any(Object),
        );
    });

    it('should also generate middleware configs', async () => {
        const services: DiscoveredService[] = [
            {
                serviceName: 'app1',
                serviceId: 'svc-1',
                labels: {
                    'federation.enable': 'true',
                    'federation.host': 'app.local',
                    'federation.port': '3000',
                },
                endpoints: [
                    {
                        nodeId: 'node-1',
                        nodeHostname: 'node1',
                        nodeIp: '192.168.1.1',
                        taskStatus: 'running',
                        taskId: 'task-1',
                    },
                ],
            },
        ];

        vi.mocked(mockDiscovery.discoverAllServices).mockResolvedValue(services);
        vi.mocked(mockFederationGen.canHandle).mockReturnValue(false);
        vi.mocked(mockMiddlewareGen.generate).mockResolvedValue({
            http: {
                middlewares: {
                    'app1-retry': {
                        retry: { attempts: 3, initialInterval: '100ms' },
                    },
                },
            },
        });
        vi.mocked(mockFileWriter.listFiles).mockResolvedValue([]);

        const orchestrator = new GlobalOrchestratorService(
            mockDiscovery,
            mockFederationGen,
            mockMiddlewareGen,
            mockFileWriter,
            mockConfig,
            mockLogger,
        );

        await orchestrator.runGenerationCycle();

        expect(mockMiddlewareGen.generate).toHaveBeenCalledOnce();
        expect(mockFileWriter.writeYaml).toHaveBeenCalledWith(
            '/data/shared/middlewares/app1.yaml',
            expect.any(Object),
        );
    });

    it('should cleanup stale files in both federation and middlewares dirs', async () => {
        const services: DiscoveredService[] = [
            {
                serviceName: 'active-app',
                serviceId: 'svc-1',
                labels: {
                    'federation.enable': 'true',
                    'federation.host': 'active.local',
                    'federation.port': '8080',
                },
                endpoints: [],
            },
        ];

        vi.mocked(mockDiscovery.discoverAllServices).mockResolvedValue(services);
        vi.mocked(mockFederationGen.canHandle).mockReturnValue(true);
        vi.mocked(mockFederationGen.generate).mockResolvedValue({
            http: {
                services: {
                    'active-app': {
                        loadBalancer: { passHostHeader: true, servers: [] },
                    },
                },
            },
        });
        vi.mocked(mockMiddlewareGen.generate).mockResolvedValue(null);
        vi.mocked(mockFileWriter.listFiles).mockResolvedValue([
            '/data/shared/federation/active-app.yaml',
            '/data/shared/middlewares/stale-middleware.yaml',
        ]);

        const orchestrator = new GlobalOrchestratorService(
            mockDiscovery,
            mockFederationGen,
            mockMiddlewareGen,
            mockFileWriter,
            mockConfig,
            mockLogger,
        );

        await orchestrator.runGenerationCycle();

        expect(mockFileWriter.deleteFile).toHaveBeenCalledWith(
            '/data/shared/middlewares/stale-middleware.yaml',
        );
        expect(mockFileWriter.deleteFile).not.toHaveBeenCalledWith(
            '/data/shared/federation/active-app.yaml',
        );
    });

    it('should handle ENOENT errors gracefully during cleanup', async () => {
        vi.mocked(mockDiscovery.discoverAllServices).mockResolvedValue([]);
        vi.mocked(mockFileWriter.listFiles).mockRejectedValue(
            Object.assign(new Error('ENOENT'), { code: 'ENOENT' }),
        );

        const orchestrator = new GlobalOrchestratorService(
            mockDiscovery,
            mockFederationGen,
            mockMiddlewareGen,
            mockFileWriter,
            mockConfig,
            mockLogger,
        );

        await expect(
            orchestrator.runGenerationCycle(),
        ).resolves.toBeUndefined();
    });
});
