import { createClient } from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-web';
import { AkinatorService } from './gen/akinator/v1/akinator_pb';

const baseUrl = import.meta.env.VITE_API_BASE ?? 'http://localhost:8080';

export const transport = createConnectTransport({
  baseUrl,
  useBinaryFormat: false
});

export const akinator = createClient(AkinatorService, transport);
