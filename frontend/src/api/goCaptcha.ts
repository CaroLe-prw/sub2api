import { apiClient } from './client'

export interface GoCaptchaChallenge {
  id: string
  image: string
  thumb: string
  width: number
  height: number
  thumb_width: number
  thumb_height: number
  thumb_x: number
  thumb_y: number
}

export async function createGoCaptchaChallenge(signal?: AbortSignal): Promise<GoCaptchaChallenge> {
  const { data } = await apiClient.post<GoCaptchaChallenge>('/auth/captcha/go/challenge', {}, { signal })
  return data
}

export async function checkGoCaptchaChallenge(
  id: string,
  point: { x: number; y: number },
  signal?: AbortSignal
): Promise<string> {
  const { data } = await apiClient.post<{ token: string }>('/auth/captcha/go/verify', { id, ...point }, { signal })
  return data.token
}
