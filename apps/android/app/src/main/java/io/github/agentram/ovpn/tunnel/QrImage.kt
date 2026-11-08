package io.github.agentram.ovpn.tunnel

import android.content.ContentResolver
import android.graphics.ImageDecoder
import android.net.Uri
import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.RGBLuminanceSource
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeReader

internal fun readQrImage(resolver: ContentResolver, uri: Uri): String {
    val bitmap = ImageDecoder.decodeBitmap(ImageDecoder.createSource(resolver, uri)) { decoder, info, _ ->
        decoder.allocator = ImageDecoder.ALLOCATOR_SOFTWARE
        val scale = maxOf(1, (maxOf(info.size.width, info.size.height).toLong() + 2047) / 2048)
        decoder.setTargetSampleSize(scale.toInt())
    }
    try {
        val pixels = IntArray(bitmap.width * bitmap.height)
        bitmap.getPixels(pixels, 0, bitmap.width, 0, 0, bitmap.width, bitmap.height)
        val image = RGBLuminanceSource(bitmap.width, bitmap.height, pixels)
        return QRCodeReader().decode(BinaryBitmap(HybridBinarizer(image)), mapOf(DecodeHintType.TRY_HARDER to true)).text
    } finally {
        bitmap.recycle()
    }
}
