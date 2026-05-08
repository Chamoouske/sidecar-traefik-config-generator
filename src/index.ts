/**
 * Sidecar de Federação Traefik - Bootstrap Principal
 *
 * Entrypoint do sidecar. Orquestra a inicialização de todos os módulos
 * de forma mode-aware, suportando GENERATION_MODE=all|global|local.
 *
 * 1. Carregamento de configuração do ambiente (incluindo GENERATION_MODE)
 * 2. Inicialização do logger estruturado
 * 3. Conexão ao Docker daemon com retry
 * 4. Inicialização da camada de serviços (label parser, file writer, service locality)
 * 5. Instanciação de orquestradores baseada no mode (global, local ou ambos)
 * 6. ModeOrchestratorService como fachada
 * 7. Servidor HTTP para health check
 * 8. Ciclo de geração inicial
 * 9. Polling periódico
 * 10. Signal handlers para graceful shutdown
 */

import { AppConfigService } from './config/index.js';
import { PinoLogger } from './logger/index.js';
import { DockerClientService } from './docker/DockerClient.js';
import { LabelParserService } from './services/LabelParserService.js';
import { SwarmDiscoveryService } from './services/SwarmDiscoveryService.js';
import { LocalDiscoveryService } from './services/LocalDiscoveryService.js';
import { ServiceLocalityService } from './services/ServiceLocalityService.js';
import { FileWriterService } from './filesystem/FileWriterService.js';
import { FederationConfigGeneratorService } from './generators/FederationConfigGeneratorService.js';
import { LocalConfigGeneratorService } from './generators/LocalConfigGeneratorService.js';
import { MiddlewareConfigGeneratorService } from './generators/MiddlewareConfigGeneratorService.js';
import { GlobalOrchestratorService } from './orchestration/GlobalOrchestratorService.js';
import { LocalOrchestratorService } from './orchestration/LocalOrchestratorService.js';
import { ModeOrchestratorService } from './orchestration/ModeOrchestratorService.js';
import { createServer, IncomingMessage, ServerResponse } from 'node:http';
import { GenerationMode, AppConfig, EnvVars } from './types/config.js';

// ─── Bootstrap ─────────────────────────────────────────────────────────────────

async function main(): Promise<void> {
    // 1. Carregar configuração do ambiente
    const configService = new AppConfigService(process.env as EnvVars);
    configService.load();
    configService.validate();
    const config = configService.get();
    const { mode } = config;

    // 2. Inicializar logger
    const logger = new PinoLogger(config);
    logger.info('Starting Traefik federation sidecar', {
        node: config.node.hostname,
        mode,
    });

    // 3. Conectar ao Docker com retry
    const dockerClient = new DockerClientService(config, logger);
    try {
        await dockerClient.connect();
    } catch (err) {
        logger.fatal('Failed to connect to Docker daemon', { error: (err as Error).message });
        process.exit(1);
    }

    // 4. Serviços compartilhados (sempre necessários)
    const labelParser = new LabelParserService();
    const fileWriter = new FileWriterService(config, logger);
    const serviceLocality = new ServiceLocalityService(config);

    // 5. Construir orquestradores específicos baseado no mode
    const hasGlobalMode = mode === 'all' || mode === 'global';
    const hasLocalMode = mode === 'all' || mode === 'local';

    let globalOrchestrator: GlobalOrchestratorService | undefined;
    let localOrchestrator: LocalOrchestratorService | undefined;

    // Global orchestrator (manager-only: federação + middlewares)
    if (hasGlobalMode) {
        const discovery = new SwarmDiscoveryService(dockerClient, labelParser, logger, config);
        const federationGenerator = new FederationConfigGeneratorService(
            serviceLocality,
            labelParser,
            logger,
        );
        const middlewareGenerator = new MiddlewareConfigGeneratorService(
            labelParser,
            logger,
            config.federation.circuitBreakerThreshold,
        );

        globalOrchestrator = new GlobalOrchestratorService(
            discovery,
            federationGenerator,
            middlewareGenerator,
            fileWriter,
            config,
            logger,
        );

        logger.info('Global orchestrator initialized (federation + middlewares)');
    }

    // Local orchestrator (any node: rotas node-specific)
    if (hasLocalMode) {
        const localDiscovery = new LocalDiscoveryService(dockerClient, labelParser, config, logger);
        const localGenerator = new LocalConfigGeneratorService(
            serviceLocality,
            labelParser,
            config,
            logger,
        );

        localOrchestrator = new LocalOrchestratorService(
            localDiscovery,
            localGenerator,
            fileWriter,
            config,
            logger,
        );

        logger.info('Local orchestrator initialized (node-specific routes)');
    }

    // 6. Mode orchestrator (fachada que decide o que executar)
    const orchestrator = new ModeOrchestratorService(
        mode,
        logger,
        globalOrchestrator,
        localOrchestrator,
    );

    // 7. Garantir que diretórios de saída existam (apenas os relevantes ao mode)
    await ensureDirectories(config, fileWriter, mode, logger);

    // 8. Servidor HTTP para health check
    const server = createServer((req: IncomingMessage, res: ServerResponse) => {
        handleHealthRequest(req, res, dockerClient, config);
    });

    await startServer(server, config, logger);

    // 9. Ciclo de geração inicial
    logger.info('Running initial generation cycle');
    try {
        await orchestrator.runGenerationCycle();
    } catch (err) {
        logger.error('Initial generation cycle failed', { error: (err as Error).message });
    }

    // 10. Polling periódico
    const pollTimer = setInterval(async () => {
        logger.debug('Running generation cycle (polling)');
        try {
            await orchestrator.runGenerationCycle();
        } catch (err) {
            logger.error('Generation cycle failed', { error: (err as Error).message });
        }
    }, config.docker.pollIntervalMs);

    logger.info('Polling configured', { intervalMs: config.docker.pollIntervalMs });

    // 11. Handler de reconexão do Docker
    dockerClient.onReconnect(async () => {
        logger.info('Docker reconnected, running generation cycle');
        try {
            await orchestrator.runGenerationCycle();
        } catch (err) {
            logger.error('Generation cycle after reconnect failed', { error: (err as Error).message });
        }
    });

    // 12. Graceful shutdown
    const shutdown = createShutdownHandler(server, pollTimer, dockerClient, logger);
    process.on('SIGTERM', shutdown);
    process.on('SIGINT', shutdown);

    logger.info('Sidecar started successfully', { mode });
}

// ─── Helpers ────────────────────────────────────────────────────────────────────

/**
 * Garante que apenas os diretórios relevantes para o mode existam.
 *
 * - Modo global ou all: diretórios de federação e middlewares
 * - Modo local ou all: diretório de configuração local
 *
 * @param config - Configuração da aplicação
 * @param fileWriter - Serviço de escrita de arquivos
 * @param mode - Modo de geração ativo
 * @param logger - Logger para registro de eventos
 */
async function ensureDirectories(
    config: AppConfig,
    fileWriter: FileWriterService,
    mode: GenerationMode,
    logger: PinoLogger,
): Promise<void> {
    if (mode === 'global' || mode === 'all') {
        await fileWriter.ensureDirectory(config.directories.federation);
        await fileWriter.ensureDirectory(config.directories.middlewares);
        logger.debug('Ensured shared directories exist');
    }

    if (mode === 'local' || mode === 'all') {
        await fileWriter.ensureDirectory(config.directories.localGenerated);
        logger.debug('Ensured local directory exists');
    }
}

/**
 * Handler do endpoint de health check.
 *
 * GET /health
 * - 200 + status "healthy" se Docker conectado
 * - 503 + status "degraded" se Docker desconectado
 * - 404 para qualquer outra rota ou método
 *
 * Inclui o mode atual na resposta para facilitar debugging.
 */
function handleHealthRequest(
    req: IncomingMessage,
    res: ServerResponse,
    dockerClient: DockerClientService,
    config: AppConfig,
): void {
    if (req.url !== config.server.healthEndpoint || req.method !== 'GET') {
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
        mode: config.mode,
        dockerConnected: isConnected,
        timestamp: new Date().toISOString(),
    }));
}

/**
 * Inicia o servidor HTTP e aguarda o callback de listen.
 *
 * @param server - Instância do servidor HTTP
 * @param config - Configuração da aplicação (porta e endpoint)
 * @param logger - Logger para registro de eventos
 * @returns Promise que resolve quando o servidor estiver ouvindo
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
 *
 * @param server - Instância do servidor HTTP
 * @param pollTimer - Timer do polling periódico
 * @param dockerClient - Cliente Docker
 * @param logger - Logger para registro de eventos
 * @returns Função assíncrona de shutdown
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
