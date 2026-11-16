// 文件作用说明：用macOS CoreText将GoodChat字标转为路径，导出不依赖查看者字体的SVG母版。
import Foundation
import CoreText
import CoreGraphics

// number 固定输出小数点和精度，避免系统地区设置改变SVG路径语法。
func number(_ value: CGFloat) -> String {
    return String(format: "%.3f", locale: Locale(identifier: "en_US_POSIX"), Double(value))
}

let font = CTFontCreateWithName("Arial-BoldMT" as CFString, 80, nil)
let characters = Array("GoodChat".utf16)
var glyphs = [CGGlyph](repeating: 0, count: characters.count)
guard CTFontGetGlyphsForCharacters(font, characters, &glyphs, characters.count) else {
    fatalError("字标字体缺少字符")
}
var advances = [CGSize](repeating: .zero, count: glyphs.count)
CTFontGetAdvancesForGlyphs(font, .horizontal, glyphs, &advances, glyphs.count)
let outline = CGMutablePath()
var cursor: CGFloat = 0
for index in glyphs.indices {
    guard let glyph = CTFontCreatePathForGlyph(font, glyphs[index], nil) else {
        fatalError("字形无法转为矢量路径")
    }
    outline.addPath(glyph, transform: CGAffineTransform(translationX: cursor, y: 0))
    cursor += advances[index].width - 1
}
let bounds = outline.boundingBoxOfPath
var commands: [String] = []
// point 将字体左下角坐标转换为SVG左上角坐标，并移除字形外围空白。
func point(_ value: CGPoint) -> String {
    return "\(number(value.x - bounds.minX)) \(number(bounds.maxY - value.y))"
}
outline.applyWithBlock { pointer in
    let element = pointer.pointee
    switch element.type {
    case .moveToPoint: commands.append("M" + point(element.points[0]))
    case .addLineToPoint: commands.append("L" + point(element.points[0]))
    case .addQuadCurveToPoint: commands.append("Q" + point(element.points[0]) + " " + point(element.points[1]))
    case .addCurveToPoint: commands.append("C" + point(element.points[0]) + " " + point(element.points[1]) + " " + point(element.points[2]))
    case .closeSubpath: commands.append("Z")
    @unknown default: fatalError("遇到不支持的字形曲线")
    }
}
print("<!-- 文件作用说明：GoodChat名称的转曲字形母版，不包含或分发字体文件。 -->")
print("<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 \(number(bounds.width)) \(number(bounds.height))\"><path fill=\"currentColor\" d=\"\(commands.joined(separator: " "))\"/></svg>")
