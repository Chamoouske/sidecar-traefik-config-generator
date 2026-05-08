/**
 * Sidecar de Federação Traefik - Bootstrap Principal
 *
 * Entrypoint do sidecar. Orquestra a inicialização de todos os módulos:
 * 1. Carregamento de configuração do ambiente
 * 2. Inicialização do logger estruturado
 * 3. Conexão ao Docker daemon com retry
 * 4. Inicialização da camada de serviços (label parser, file writer, service locality)
 * 5. Inicialização dos geradores (federação, local, middleware)
 * 6. Inicialização da descoberta Swarm
 * 7. Inicialização do orquestrador de configuração
 * 8. Servidor HTTP para health check
 * 9. Ciclo de geração inicial
 * 10. Polling periódico
 * 11. Signal handlers para graceful shutdown
 */

import { AppConfigService } from './config/index.js';
import { EnvVars } from './types/config.js';
import { PinoLogger } from './logger/index.js';
import { DockerClientService } from './docker/DockerClient.js';
import { LabelParserService } from './services/LabelParserService.js';
import { SwarmDiscoveryService } from './services/SwarmDiscoveryService.js';
import { ServiceLocalityService } from './services/ServiceLocalityService.js';
import { EventEmitterService } from './services/EventEmitterService.js';
import { FileWriterService } from './filesystem/FileWriterService.js';
import { FederationConfigGeneratorService } from './generators/FederationConfigGeneratorService.js';
import { LocalConfigGeneratorService } from './generators/LocalConfigGeneratorService.js';
import { MiddlewareConfigGeneratorService } from './generators/MiddlewareConfigGeneratorService.js';
import { ConfigOrchestratorService } from './services/ConfigOrchestratorService.js';
import { createServer, IncomingMessage, ServerResponse } from 'node:http';
import { AppConfig } from './types/config.js';

// ─── Bootstrap ─────────────────────────────────────────────────────────────────

async function main(): Promise<void> {
    // 1. Carregar configuração do ambiente
    const configService = new AppConfigService(process.env as EnvVars);
    configService.load();
    configService.validate();
    const config = configService.get();

    // 2. Inicializar logger
    const logger = new PinoLogger(config);
    logger.info('Starting Traefik federation sidecar', { node: config.node.hostname });

    // 3. Conectar ao Docker com retry
    const dockerClient = new DockerClientService(config, logger);
    try {
        await dockerClient.connect();
    } catch (err) {
        logger.fatal('Failed to connect to Docker daemon', { error: (err as Error).message });
        process.exit(1);
    }

    // 4. Inicializar serviços
    const labelParser = new LabelParserService();
    const fileWriter = new FileWriterService(config, logger);
    const serviceLocality = new ServiceLocalityService(config);
    const eventEmitter = new EventEmitterService();

    // 5. Inicializar descoberta Swarm
    const discovery = new SwarmDiscoveryService(dockerClient, labelParser, logger);

    // 6. Inicializar geradores
    const federationGenerator = new FederationConfigGeneratorService(
        serviceLocality,
        labelParser,
        logger,
    );
    const localGenerator = new LocalConfigGeneratorService(
        serviceLocality,
        labelParser,
        config,
        logger,
    );
    const middlewareGenerator = new MiddlewareConfigGeneratorService(
        labelParser,
        logger,
        config.federation.circuitBreakerThreshold,
    );

    // 7. Inicializar orquestrador de configuração
    const orchestrator = new ConfigOrchestratorService(
        discovery,
        federationGenerator,
        localGenerator,
        middlewareGenerator,
        fileWriter,
        config,
        logger,
    );

    // 8. Garantir que diretórios de saída existam
    logger.info('Ensuring output directories exist');
    await ensureDirectories(config, fileWriter);

    // 9. Servidor HTTP para health check
    const server = createServer((req: IncomingMessage, res: ServerResponse) => {
        handleHealthRequest(req, res, dockerClient);
    });

    await startServer(server, config, logger);

    // 10. Ciclo de geração inicial
    logger.info('Running initial generation cycle');
    try {
        await orchestrator.runGenerationCycle();
    } catch (err) {
        logger.error('Initial generation cycle failed', { error: (err as Error).message });
    }

    // 11. Polling periódico
    const pollTimer = setInterval(async () => {
        logger.debug('Running generation cycle (polling)');
        try {
            await orchestrator.runGenerationCycle();
        } catch (err) {
            logger.error('Generation cycle failed', { error: (err as Error).message });
        }
    }, config.docker.pollIntervalMs);

    logger.info('Polling configured', { intervalMs: config.docker.pollIntervalMs });

    // 12. Handler de reconexão do Docker
    dockerClient.onReconnect(async () => {
        logger.info('Docker reconnected, running generation cycle');
        try {
            await orchestrator.runGenerationCycle();
        } catch (err) {
            logger.error('Generation cycle after reconnect failed', { error: (err as Error).message });
        }
    });

    // 13. Graceful shutdown
    const shutdown = createShutdownHandler(server, pollTimer, dockerClient, logger);
    process.on('SIGTERM', shutdown);
    process.on('SIGINT', shutdown);

    logger.info('Sidecar started successfully');
}

// ─── Helpers ────────────────────────────────────────────────────────────────────

/**
 * Garante que todos os diretórios de saída existam no sistema de arquivos.
 */
async function ensureDirectories(config: AppConfig, fileWriter: FileWriterService): Promise<void> {
    const dirs = [
        { path: config.directories.federation, label: 'federation' },
        { path: config.directories.middlewares, label: 'middlewares' },
        { path: config.directories.localGenerated, label: 'local-generated' },
    ];

    for (const dir of dirs) {
        await fileWriter.ensureDirectory(dir.path);
    }
}

/**
 * Handler do endpoint de health check.
 *
 * GET /health
 * - 200 + {"status":"healthy","dockerConnected":true} se Docker conectado
 * - 503 + {"status":"degraded","dockerConnected":false} se Docker desconectado
 * - 404 para qualquer outra rota ou método
 */
function handleHealthRequest(
    req: IncomingMessage,
    res: ServerResponse,
    dockerClient: DockerClientService,
): void {
    if (req.url !== '/health' || req.method !== 'GET') {
        res.writeHead(404);
        res.end('Not Found');
        return;
    }

    const isConnected = dockerClient.isConnected();
    const status = isConnected ? 'healthy' : 'degraded';
    const statusCode = isConnected ? 200 : 503;

    res.writeHead(statusCode, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({
        status,
        dockerConnected: isConnected,
        timestamp: new Date().toISOString(),
    }));
}

/**
 * Inicia o servidor HTTP e aguarda o callback de listen.
 */
function startServer(
    server: ReturnType<typeof createServer>,
    config: AppConfig,
    logger: PinoLogger,
): Promise<void> {
    return new Promise((resolve) => {
        server.listen(config.server.port, () => {
            logger.info('Health endpoint listening', {
                port: config.server.port,
                endpoint: config.server.healthEndpoint,
            });
            resolve();
        });
    });
}

/**
 * Cria um handler de graceful shutdown que:
 * 1. Para o timer de polling
 * 2. Fecha o servidor HTTP (para de aceitar novas conexões)
 * 3. Desconecta o cliente Docker
 * 4. Loga cada etapa
 * 5. Sai com código 0
 */
function createShutdownHandler(
    server: ReturnType<typeof createServer>,
    pollTimer: NodeJS.Timeout,
    dockerClient: DockerClientService,
    logger: PinoLogger,
): () => Promise<void> {
    let shuttingDown = false;

    return async () => {
        // Evita execução concorrente do shutdown
        if (shuttingDown) {
            logger.debug('Shutdown already in progress, ignoring signal');
            return;
        }
        shuttingDown = true;

        logger.info('Shutting down gracefully...');

        // 1. Parar o timer de polling
        clearInterval(pollTimer);
        logger.debug('Polling timer stopped');

        // 2. Fechar o servidor HTTP (para de aceitar novas conexões)
        logger.debug('Closing HTTP server...');
        server.close(() => {
            logger.info('HTTP server closed');
        });

        // 3. Desconectar o cliente Docker
        logger.debug('Disconnecting Docker client...');
        try {
            await dockerClient.disconnect();
            logger.info('Docker client disconnected');
        } catch (err) {
            logger.error('Error disconnecting Docker client', { error: (err as Error).message });
        }

        // 4. Finalizar
        logger.info('Shutdown complete');
        process.exit(0);
    };
}

// ─── Entrypoint ─────────────────────────────────────────────────────────────────

main().catch((err) => {
    console.error('Fatal error during startup:', err);
    process.exit(1);
});
