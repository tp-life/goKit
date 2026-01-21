/**
 * 压缩图片至指定大小（KB）
 * @param {File} file - 原始图片文件
 * @param {number} maxSizeKB - 最大文件大小（KB），默认 500
 * @returns {Promise<Blob>} 压缩后的图片 Blob
 */
export function compressImage(file, maxSizeKB = 500) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.readAsDataURL(file)
    
    reader.onload = (event) => {
      const img = new Image()
      img.src = event.target.result
      
      img.onload = () => {
        const canvas = document.createElement('canvas')
        let width = img.width
        let height = img.height
        let quality = 0.9

        // 计算压缩后的尺寸（最大 1920px）
        const maxDimension = 1920
        if (width > maxDimension || height > maxDimension) {
          if (width > height) {
            height = (height * maxDimension) / width
            width = maxDimension
          } else {
            width = (width * maxDimension) / height
            height = maxDimension
          }
        }

        canvas.width = width
        canvas.height = height

        const ctx = canvas.getContext('2d')
        ctx.drawImage(img, 0, 0, width, height)

        // 逐步降低质量直到文件大小符合要求
        const compress = () => {
          canvas.toBlob(
            (blob) => {
              if (!blob) {
                reject(new Error('图片压缩失败'))
                return
              }

              const sizeKB = blob.size / 1024
              if (sizeKB <= maxSizeKB || quality <= 0.1) {
                resolve(blob)
              } else {
                quality -= 0.1
                compress()
              }
            },
            'image/jpeg',
            quality
          )
        }
        
        compress()
      }
      
      img.onerror = () => {
        reject(new Error('图片加载失败'))
      }
    }
    
    reader.onerror = () => {
      reject(new Error('文件读取失败'))
    }
  })
}
