const API_BASE = '/api/v1'

export function useAPI() {
  const request = async (url, options = {}) => {
    const response = await fetch(`${API_BASE}${url}`, {
      headers: {
        'Content-Type': 'application/json',
        ...options.headers,
      },
      ...options,
    })

    if (!response.ok) {
      throw new Error(`API error: ${response.statusText}`)
    }

    const data = await response.json()
    if (data.code !== 0) {
      throw new Error(data.msg || 'API request failed')
    }

    return data.data
  }

  return {
    getComparisons: () => request('/comparison'),
    getComparison: (symbol) => request(`/comparison/${symbol}`),
    getArbitrageOpportunities: () => request('/arbitrage'),
    getHighArbitrageOpportunities: () => request('/arbitrage/high'),
    updateThreshold: (threshold) => request('/threshold', {
      method: 'PUT',
      body: JSON.stringify(threshold),
    }),
  }
}
