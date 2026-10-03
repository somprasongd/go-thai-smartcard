import {
  SmartCardClient, SmartCardClientError, type Personal,
} from '@somprasongd/thai-smartcard-client';

// Your application owns form updates and its handling of previously read data.
export async function attachCardReader(fillForm: (personal: Personal) => void) {
  const client = new SmartCardClient({url: 'ws://127.0.0.1:9898/ws'});
  client.on('card', data => { if (data.personal) fillForm(data.personal); });
  client.on('error', error => console.error(error.code));
  try {
    await client.connect();
  } catch (error) {
    if (!(error instanceof SmartCardClientError)) throw error;
    // Connection events can still report later background reconnect success.
    console.error(error.code);
  }
  return client;
}
