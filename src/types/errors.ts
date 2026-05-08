/**
 * Hierarquia de erros do Sidecar de Federação Traefik.
 */

export class SidecarError extends Error {
    constructor(message: string, public readonly cause?: Error) {
        super(message);
        this.name = 'SidecarError';
    }
}

export class DockerConnectionError extends SidecarError {
    constructor(message: string, cause?: Error) {
        super(message, cause);
        this.name = 'DockerConnectionError';
    }
}

export class ConfigValidationError extends SidecarError {
    constructor(message: string) {
        super(message);
        this.name = 'ConfigValidationError';
    }
}

export class FileWriteError extends SidecarError {
    constructor(
        message: string,
        public readonly filePath: string,
        cause?: Error
    ) {
        super(message, cause);
        this.name = 'FileWriteError';
    }
}

export class DiscoveryError extends SidecarError {
    constructor(message: string, cause?: Error) {
        super(message, cause);
        this.name = 'DiscoveryError';
    }
}
